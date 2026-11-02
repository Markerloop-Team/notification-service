package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"notification-service/internal/handlers"
)

// SDL-322: TestEmail рассылает письмо на ПРОИЗВОЛЬНЫЙ адрес из тела запроса.
// Она жила в публичной группе `/email` и попадала в спеку как публичная
// операция, хотя шлюз её никогда не публиковал — снаружи это читалось как
// «объявленный роут недостижим» (расхождение, найденное джобой gw-contract,
// SDL-319).
//
// Тест исполняет НАСТОЯЩИЙ роутер, а не сверяет строки: сборка не доказывает
// ничего о том, какие пути зарегистрированы и что стоит перед ними. Проверяем
// три утверждения сразу — новый путь существует и закрыт ключом, старый путь
// не отвечает вовсе, а публичные email-ручки остались на месте.
func newTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(io_discard{})
	// Хендлеру для этих проверок не нужен рабочий SMTP: ни один кейс ниже не
	// доходит до отправки — все упираются в роутинг или в middleware.
	return SetupRoutes(&handlers.NotificationHandler{}, logger, "test-service-key")
}

type io_discard struct{}

func (io_discard) Write(p []byte) (int, error) { return len(p), nil }

func do(t *testing.T, r *gin.Engine, method, path, key string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(`{"to":"probe@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("X-Service-Key", key)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestTestEmailIsServiceToServiceOnly(t *testing.T) {
	r := newTestRouter()

	t.Run("новый путь без ключа отвергается, а не пропускается", func(t *testing.T) {
		w := do(t, r, http.MethodPost, "/api/v1/notifications/internal/email/test", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("без X-Service-Key ожидался 401, получен %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("новый путь с неверным ключом отвергается", func(t *testing.T) {
		w := do(t, r, http.MethodPost, "/api/v1/notifications/internal/email/test", "wrong-key")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("с чужим ключом ожидался 401, получен %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("старый публичный путь больше не зарегистрирован", func(t *testing.T) {
		w := do(t, r, http.MethodPost, "/api/v1/notifications/email/test", "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("старый публичный путь обязан исчезнуть, получен %d: %s", w.Code, w.Body.String())
		}
	})
}

// Соседние email-ручки публикует шлюз (под тем же ключом) —
// правка SDL-322 не должна была их задеть. Смотрим таблицу роутов, а не
// ответы: закрытость ключом проверяет TestNotificationRoutesRequireServiceKey,
// а вызов с пустым сервисом внутри доказывал бы только работу gin.Recovery.
func TestEmailRoutesStayRegistered(t *testing.T) {
	registered := map[string]bool{}
	for _, ri := range newTestRouter().Routes() {
		registered[ri.Method+" "+ri.Path] = true
	}

	for _, want := range []string{
		"POST /api/v1/notifications/email/send",
		"POST /api/v1/notifications/email/send-verification",
		"POST /api/v1/notifications/email/send-welcome",
		"POST /api/v1/notifications/email/send-password-reset",
		"POST /api/v1/notifications/telegram/send",
		"POST /api/v1/notifications/deploy",
	} {
		if !registered[want] {
			t.Errorf("%s пропал из роутера", want)
		}
	}

	if registered["POST /api/v1/notifications/email/test"] {
		t.Error("старый публичный путь всё ещё зарегистрирован")
	}
	if !registered["POST /api/v1/notifications/internal/email/test"] {
		t.Error("новый сервис-сервисный путь не зарегистрирован")
	}
}

// Вся группа /api/v1/notifications под X-Service-Key: до этой правки ключ
// проверял только шлюз, и под в кластере слал письма и сообщения в Telegram
// без него. Исполняем роутер: без ключа и с чужим ключом — 401 до хендлера,
// а health остаётся открытым для проб.
func TestNotificationRoutesRequireServiceKey(t *testing.T) {
	r := newTestRouter()

	for _, path := range []string{
		"/api/v1/notifications/email/send",
		"/api/v1/notifications/email/send-verification",
		"/api/v1/notifications/email/send-welcome",
		"/api/v1/notifications/email/send-password-reset",
		"/api/v1/notifications/telegram/send",
		"/api/v1/notifications/deploy",
		"/api/v1/notifications/internal/email/workspace-invite",
	} {
		for _, key := range []string{"", "wrong-key"} {
			if w := do(t, r, http.MethodPost, path, key); w.Code != http.StatusUnauthorized {
				t.Errorf("%s с ключом %q: ожидался 401, получен %d", path, key, w.Code)
			}
		}
	}

	if w := do(t, r, http.MethodGet, "/health", ""); w.Code != http.StatusOK {
		t.Errorf("/health без ключа: ожидался 200, получен %d", w.Code)
	}
}
