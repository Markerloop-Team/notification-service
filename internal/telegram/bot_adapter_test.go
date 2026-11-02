package telegram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// SDL-346 (EP-56): доставка в бот идёт с ретраями. Прежние
// характеризационные тесты SDL-343 фиксировали ДЕФЕКТНОЕ поведение
// «ровно одна попытка» и покраснели от этой правки ровно так, как
// обещали их комментарии и строка в REGRESSION.md, — ожидания здесь
// перевёрнуты на новую семантику.
//
// Контракт с приёмником (markerloop-bot SDL-345): Idempotency-Key один на
// все попытки одной доставки; 409 от бота = «та же доставка в полёте» и
// ретраится; 4xx (кроме 409) не ретраится.

func newTestAdapter(t *testing.T, url string, attempts int) *BotAdapter {
	t.Helper()
	log := logrus.New()
	log.SetOutput(io.Discard)
	// backoff 1ms — тесты не должны спать сотнями миллисекунд
	return NewBotAdapter(url, "test-service-key", 2*time.Second, attempts, time.Millisecond, log)
}

// Транспортная ошибка (обрыв соединения — так выглядит под бота, умерший
// посреди обработки) ретраится до исчерпания попыток.
func TestTransportErrorIsRetried(t *testing.T) {
	var attempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, err := hj.Hijack()
			if err == nil {
				_ = conn.Close()
				return
			}
		}
		panic(http.ErrAbortHandler)
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL, 3)
	err := a.SendDeployNotificationPayload(context.Background(), DeployPayload{})

	if err == nil {
		t.Fatal("ожидалась ошибка после исчерпания попыток, получен nil")
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("попыток %d, ожидалось 3", got)
	}
}

// connection refused (ровно случай инцидента MARKERLOOP-GO-6/7): все попытки
// исчерпаны — ошибка доходит до вызывающего, не гасится.
func TestConnectionRefusedSurfacesAfterRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := srv.URL
	srv.Close()

	a := newTestAdapter(t, deadURL, 3)
	err := a.SendDeployNotificationPayload(context.Background(), DeployPayload{})

	if err == nil {
		t.Fatal("ожидалась ошибка транспорта, получен nil — сообщение считалось бы доставленным")
	}
}

// 5xx от бота ретраится: бот мог упасть посреди обработки.
func TestServerErrorIsRetried(t *testing.T) {
	var attempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"success":false}`))
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL, 3)
	err := a.SendDeployNotificationPayload(context.Background(), DeployPayload{})

	if err == nil {
		t.Fatal("ожидалась ошибка на 502, получен nil")
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("попыток %d, ожидалось 3", got)
	}
}

// 409 — контракт SDL-345: «та же доставка уже в полёте» — ретраится.
func TestConflictIsRetriedPerBotContract(t *testing.T) {
	var attempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"success":false,"error":"conflict"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{"duplicate":true}}`))
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL, 3)
	if err := a.SendDeployNotificationPayload(context.Background(), DeployPayload{}); err != nil {
		t.Fatalf("409→409→200 обязан закончиться успехом, получено: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("попыток %d, ожидалось 3", got)
	}
}

// Прочие 4xx НЕ ретраятся: ошибку запроса повтор не исправит.
func TestClientErrorIsNotRetried(t *testing.T) {
	var attempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"success":false}`))
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL, 3)
	err := a.SendDeployNotificationPayload(context.Background(), DeployPayload{})

	if err == nil {
		t.Fatal("ожидалась ошибка на 400, получен nil")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("попыток %d, ожидалась 1 (4xx не ретраится)", got)
	}
}

// Успех со второй попытки — nil: ретрай реально спасает доставку
// (сценарий инцидента: под бота рестартовал и поднялся).
func TestSuccessOnSecondAttempt(t *testing.T) {
	var attempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL, 3)
	if err := a.SendDeployNotificationPayload(context.Background(), DeployPayload{}); err != nil {
		t.Fatalf("успех со 2-й попытки вернул ошибку: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("попыток %d, ожидалось 2", got)
	}
}

// Idempotency-Key: присутствует, стабилен между ВСЕМИ попытками одной
// доставки (иначе дедуп бота — no-op) и различается между доставками.
// X-Service-Key уходит в каждой попытке.
func TestIdempotencyKeyStableAcrossAttempts(t *testing.T) {
	var mu sync.Mutex
	var keys []string
	var svcKeys []string
	var attempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		keys = append(keys, r.Header.Get(IdempotencyHeader))
		svcKeys = append(svcKeys, r.Header.Get("X-Service-Key"))
		mu.Unlock()
		if atomic.AddInt32(&attempts, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL, 3)
	if err := a.SendDeployNotificationPayload(context.Background(), DeployPayload{}); err != nil {
		t.Fatalf("доставка: %v", err)
	}

	if len(keys) != 3 {
		t.Fatalf("попыток %d, ожидалось 3", len(keys))
	}
	hex32 := regexp.MustCompile(`^[0-9a-f]{32}$`)
	if !hex32.MatchString(keys[0]) {
		t.Fatalf("ключ не 32-hex: %q", keys[0])
	}
	if keys[0] != keys[1] || keys[1] != keys[2] {
		t.Fatalf("ключ нестабилен между попытками: %v — дедуп бота был бы no-op", keys)
	}
	for i, k := range svcKeys {
		if k != "test-service-key" {
			t.Errorf("попытка %d: X-Service-Key не передан (%q)", i+1, k)
		}
	}

	// Вторая доставка — другой ключ.
	atomic.StoreInt32(&attempts, 2) // сразу успех
	if err := a.SendDeployNotificationPayload(context.Background(), DeployPayload{}); err != nil {
		t.Fatalf("вторая доставка: %v", err)
	}
	mu.Lock()
	last := keys[len(keys)-1]
	mu.Unlock()
	if last == keys[0] {
		t.Errorf("ключ второй доставки совпал с первой: %q — разные события давились бы как дубли", last)
	}
}

// Отменённый контекст прерывает ретраи быстро, не досиживая backoff.
func TestContextCancelStopsRetries(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	log := logrus.New()
	log.SetOutput(io.Discard)
	// большой backoff: без уважения к ctx тест бы завис на секунды
	a := NewBotAdapter(srv.URL, "k", 2*time.Second, 5, 10*time.Second, log)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := a.SendDeployNotificationPayload(ctx, DeployPayload{})
	if err == nil {
		t.Fatal("ожидалась ошибка отменённого контекста")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("ретраи не прервались по ctx: заняло %v", elapsed)
	}
}

// SDL-351: бюджет ретраев обязан переживать штатную замену пода бота.
//
// Это не тест на «сколько раз дёрнулся HTTP», а тест на арифметику
// расписания: реальный сбой 21 Aug 2026 (Sentry MARKERLOOP-GO-37) состоял
// именно в том, что три попытки с базой 500ms укладывались в ~1.5s и
// заканчивались задолго до того, как новый под успевал принять соединение,
// хотя totalDeliveryBudget отпускал на это 25s. Проверяем оба конца:
// последняя попытка стартует достаточно поздно, чтобы rolling-update успел
// завершиться, и при этом внутри бюджета — иначе бюджет режет её сам, и
// заявленное число попыток становится фикцией.
func TestRetryScheduleCoversPodRestart(t *testing.T) {
	// Значения по умолчанию: см. config.LoadConfig (TELEGRAM_RETRY_ATTEMPTS,
	// TELEGRAM_RETRY_BACKOFF) и подстановку в NewBotAdapter.
	const (
		attempts = 6
		backoff  = 500 * time.Millisecond
		// Нижняя граница окна rolling-update, которое обязаны переживать:
		// terminationGracePeriod пода бота плюс старт нового.
		minRestartWindow = 10 * time.Second
	)

	// Сумма пауз перед попыткой n: base * (2^(n-1) - 1). Джиттер ±20% на
	// каждой паузе учитываем худшим случаем в обе стороны.
	var nominal time.Duration
	for attempt := 2; attempt <= attempts; attempt++ {
		nominal += backoff << (attempt - 2)
	}
	earliest := nominal - nominal/4 // джиттер утянул все паузы вниз
	latest := nominal + nominal/4   // и вверх

	if earliest < minRestartWindow {
		t.Errorf("последняя попытка стартует не позже %v даже при %d попытках — окно перезапуска пода (%v) не покрыто",
			earliest, attempts, minRestartWindow)
	}
	if latest >= totalDeliveryBudget {
		t.Errorf("расписание ретраев (%v в худшем случае) не влезает в totalDeliveryBudget (%v): бюджет обрежет последние попытки",
			latest, totalDeliveryBudget)
	}
}

// Транспортная ошибка ретраится ровно столько раз, сколько задано, и число
// это теперь 6 по умолчанию — регрессия на случай, если кто-то вернёт 3,
// не тронув расписание выше.
func TestConnectionRefusedRetriedToConfiguredAttempts(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		// Обрыв соединения — так с точки зрения клиента выглядит под,
		// которого пересоздаёт rolling-update.
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("httptest сервер не отдал Hijacker")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		conn.Close()
	}))
	defer srv.Close()

	// attempts=0 → подстановка безопасного значения по умолчанию.
	a := newTestAdapter(t, srv.URL, 0)
	if err := a.SendMessage(context.Background(), "42", "заголовок", "текст"); err == nil {
		t.Fatal("ожидалась ошибка после исчерпания попыток, получен nil")
	}
	if got := atomic.LoadInt32(&hits); got != 6 {
		t.Errorf("попыток %d, ожидалось 6 (значение по умолчанию SDL-351)", got)
	}
}
