package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"

	"gitlab.com/markerloop/go-sdk/pkg/requestid"
)

// IdempotencyHeader — заголовок, которым помечается доставка (EP-56).
// Ключ генерируется ОДИН раз на доставку и повторяется во всех её ретраях —
// по нему бот подавляет повторную рассылку (markerloop-bot SDL-345).
// Контракт продублирован тестами обеих сторон и e2e-приёмкой SDL-347.
const IdempotencyHeader = "Idempotency-Key"

// totalDeliveryBudget ограничивает суммарное время всех попыток одной
// доставки. Services-слой передаёт context.Background() (см.
// notification_service.go), а хендлер тем временем держит HTTP-соединение
// вызывающего: WriteTimeout сервера — 30s, значит ретраи обязаны уложиться
// раньше, иначе вызывающий получит обрыв вместо ответа.
const totalDeliveryBudget = 25 * time.Second

// BotAdapter отправляет Telegram сообщения через HTTP вызовы к markerloop-bot
type BotAdapter struct {
	botServiceURL string
	serviceKey    string
	httpClient    *http.Client
	logger        *logrus.Logger
	// SDL-346: ретраи с экспоненциальным backoff. attempts — общее число
	// попыток; backoff — база паузы (растёт ×2 с попыткой, с джиттером).
	attempts int
	backoff  time.Duration
}

// NewBotAdapter создаёт новый адаптер для отправки через бот.
// attempts <= 0 и backoff <= 0 подменяются безопасными значениями — криво
// собранный конфиг не должен превращать доставку в одну попытку без пауз.
func NewBotAdapter(botServiceURL, serviceKey string, timeout time.Duration, attempts int, backoff time.Duration, logger *logrus.Logger) *BotAdapter {
	if attempts <= 0 {
		attempts = 6
	}
	if backoff <= 0 {
		backoff = 500 * time.Millisecond
	}
	return &BotAdapter{
		botServiceURL: botServiceURL,
		serviceKey:    serviceKey,
		httpClient:    &http.Client{Timeout: timeout},
		logger:        logger,
		attempts:      attempts,
		backoff:       backoff,
	}
}

// SendMessage отправляет сообщение пользователю через бот
func (a *BotAdapter) SendMessage(ctx context.Context, userID, title, message string) error {
	url := fmt.Sprintf("%s/api/v1/notifications/send", a.botServiceURL)
	body := map[string]string{
		"user_id": userID,
		"title":   title,
		"message": message,
	}
	return a.doPost(ctx, url, body)
}

// SendDeployNotificationPayload пересылает rich-payload в бот.
// Bot принимает все поля как optional (см. markerloop-bot
// telegram_handler.DeployNotification); отсутствующие в payload
// поля просто опускаются благодаря omitempty в json-тегах.
func (a *BotAdapter) SendDeployNotificationPayload(ctx context.Context, payload DeployPayload) error {
	url := fmt.Sprintf("%s/api/v1/notifications/deploy", a.botServiceURL)
	return a.doPost(ctx, url, payload)
}

// doPost доставляет payload боту с ретраями (SDL-346, EP-56).
//
// Классификация исходов попытки:
//   - транспортная ошибка (connection refused при рестарте пода бота —
//     инцидент MARKERLOOP-GO-6/7) → ретрай;
//   - 5xx → ретрай (бот упал посреди обработки);
//   - 409 → ретрай: по контракту SDL-345 это «та же доставка уже в полёте»,
//     следующая попытка получит 200 duplicate либо переиграет провал;
//   - остальные 4xx → не ретраить (ошибка запроса, повтор её не исправит);
//   - 2xx → доставлено.
//
// Idempotency-Key один на все попытки — иначе дедуп бота бесполезен.
func (a *BotAdapter) doPost(ctx context.Context, url string, payload interface{}) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("ошибка сериализации: %w", err)
	}

	idemKey := requestid.New()

	ctx, cancel := context.WithTimeout(ctx, totalDeliveryBudget)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= a.attempts; attempt++ {
		if attempt > 1 {
			// Экспоненциальный backoff с джиттером ±20%: base, 2×base, 4×base…
			delay := a.backoff << (attempt - 2)
			jitter := time.Duration(rand.Int63n(int64(delay)/2+1)) - delay/4
			select {
			case <-time.After(delay + jitter):
			case <-ctx.Done():
				return fmt.Errorf("доставка боту прервана (%d/%d попыток): %w (последняя ошибка: %v)",
					attempt-1, a.attempts, ctx.Err(), lastErr)
			}
		}

		// Тело http.Request одноразовое — на каждую попытку свой запрос.
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			return fmt.Errorf("ошибка создания запроса: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(IdempotencyHeader, idemKey)
		if a.serviceKey != "" {
			req.Header.Set("X-Service-Key", a.serviceKey)
		}
		requestid.Inject(ctx, req)

		resp, err := a.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("ошибка HTTP запроса к боту: %w", err)
			a.logger.Warnf("Доставка боту: попытка %d/%d не удалась: %v", attempt, a.attempts, err)
			continue
		}

		if resp.StatusCode < 300 {
			resp.Body.Close()
			if attempt > 1 {
				a.logger.Infof("Доставка боту удалась с попытки %d/%d", attempt, a.attempts)
			} else {
				a.logger.Debugf("Telegram запрос к %s: OK", url)
			}
			return nil
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lastErr = fmt.Errorf("бот вернул статус %d: %s", resp.StatusCode, string(body))

		if resp.StatusCode < 500 && resp.StatusCode != http.StatusConflict {
			// Ошибка запроса — повтор её не исправит.
			return lastErr
		}
		a.logger.Warnf("Доставка боту: попытка %d/%d: %v", attempt, a.attempts, lastErr)
	}

	return fmt.Errorf("доставка боту не удалась за %d попыток: %w", a.attempts, lastErr)
}
