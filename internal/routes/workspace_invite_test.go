package routes

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"notification-service/internal/config"
	"notification-service/internal/handlers"
	"notification-service/internal/services"
)

// SDL-615 (S3): ручка /internal/email/workspace-invite исполняется ЦЕЛИКОМ —
// настоящий роутер, настоящий middleware ключа, настоящий хендлер и сервис,
// подменён только SMTP. Сборка и таблица роутов не доказывают ни
// экранирования, ни того, что до письма вообще доходит.

// captureSender перехватывает письмо вместо SMTP.
type captureSender struct {
	mu   sync.Mutex
	sent []capturedMail
}

type capturedMail struct{ To, Subject, Body string }

func (c *captureSender) SendEmail(_ context.Context, to, subject, body string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, capturedMail{To: to, Subject: subject, Body: body})
	return nil
}

const (
	invitePath = "/api/v1/notifications/internal/email/workspace-invite"
	inviteKey  = "test-service-key"
)

func newInviteRouter(t *testing.T) (*gin.Engine, *captureSender) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(io_discard{})
	sender := &captureSender{}
	svc := services.NewNotificationServiceWithSender(sender, config.TelegramConfig{}, config.EmailTemplatesConfig{CompanyName: "Команда MarkerLoop"}, logger)
	h := handlers.NewNotificationHandler(*svc, logger)
	return SetupRoutes(h, logger, inviteKey), sender
}

func postInvite(t *testing.T, r *gin.Engine, key string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, invitePath, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("X-Service-Key", key)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func invitePayload(locale string) map[string]any {
	return map[string]any{
		"to":             "invitee@example.com",
		"workspace_name": `<script>alert("ws")</script> Acme & Co`,
		"inviter_name":   `<a href="https://evil.test">click</a> Bob`,
		"accept_url":     "https://test.markerloop.com/invite#token=mli_abc123",
		"expires_at":     "2026-10-04T12:34:56Z",
		"locale":         locale,
	}
}

func TestWorkspaceInvite_RequiresServiceKey(t *testing.T) {
	r, sender := newInviteRouter(t)

	t.Run("без ключа — 401", func(t *testing.T) {
		w := postInvite(t, r, "", invitePayload("en"))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("ожидался 401, получен %d: %s", w.Code, w.Body.String())
		}
	})
	t.Run("с чужим ключом — 401", func(t *testing.T) {
		w := postInvite(t, r, "wrong-key", invitePayload("en"))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("ожидался 401, получен %d: %s", w.Code, w.Body.String())
		}
	})
	if len(sender.sent) != 0 {
		t.Fatalf("без ключа письмо не должно отправляться, отправлено %d", len(sender.sent))
	}
}

func TestWorkspaceInvite_LivesOnlyUnderInternal(t *testing.T) {
	r, _ := newInviteRouter(t)
	registered := map[string]bool{}
	for _, ri := range r.Routes() {
		registered[ri.Method+" "+ri.Path] = true
	}
	if !registered["POST "+invitePath] {
		t.Errorf("%s не зарегистрирован", invitePath)
	}
	for _, leak := range []string{
		"POST /api/v1/notifications/email/workspace-invite",
		"POST /api/v1/notifications/workspace-invite",
		"POST /api/v1/notifications/email/send-workspace-invite",
	} {
		if registered[leak] {
			t.Errorf("ручка приглашения просочилась в публичную группу: %s", leak)
		}
	}
	// И живым запросом: публичный путь отвечает 404, а не 401 и не 200.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/email/workspace-invite", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("публичный путь обязан отвечать 404, получен %d", w.Code)
	}
}

func TestWorkspaceInvite_RejectsBadPayload(t *testing.T) {
	r, sender := newInviteRouter(t)
	cases := map[string]func(map[string]any){
		"missing-to":             func(p map[string]any) { delete(p, "to") },
		"bad-email":              func(p map[string]any) { p["to"] = "not-an-email" },
		"missing-workspace-name": func(p map[string]any) { delete(p, "workspace_name") },
		"missing-accept-url":     func(p map[string]any) { delete(p, "accept_url") },
		"javascript-accept-url":  func(p map[string]any) { p["accept_url"] = "javascript:alert(1)" },
		"javascript-host-url":    func(p map[string]any) { p["accept_url"] = "javascript://evil.test/%0aalert(1)" },
		"ftp-accept-url":         func(p map[string]any) { p["accept_url"] = "ftp://x/invite" },
		"http-accept-url":        func(p map[string]any) { p["accept_url"] = "http://test.markerloop.com/invite#token=mli_secret" },
		"userinfo-accept-url":    func(p map[string]any) { p["accept_url"] = "https://markerloop.com@evil.test/invite#token=mli_secret" },
		"relative-accept-url":    func(p map[string]any) { p["accept_url"] = "/invite#token=mli_secret" },
		"bad-expires-at":         func(p map[string]any) { p["expires_at"] = "tomorrow" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := invitePayload("en")
			mutate(p)
			w := postInvite(t, r, inviteKey, p)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("ожидался 400, получен %d: %s", w.Code, w.Body.String())
			}
			// accept_url несёт токен приглашения: ответ 400 не должен его эхом
			// возвращать (он уходит в логи вызывающего сервиса и в Sentry).
			for _, leak := range []string{"mli_secret", "evil.test", "alert("} {
				if strings.Contains(w.Body.String(), leak) {
					t.Errorf("ответ 400 повторяет вход (%q): %s", leak, w.Body.String())
				}
			}
		})
	}
	if len(sender.sent) != 0 {
		t.Fatalf("на невалидный вход письмо не должно отправляться, отправлено %d", len(sender.sent))
	}
}

// Subject уходит в SMTP сырым заголовком (buildMessage), поэтому нелатинская
// тема обязана быть RFC 2047-кодирована: сам заголовок — только ASCII, а
// декодируется в константу локали. Подсадка «убрать mime.QEncoding» красит
// этот тест на ru и kk.
func TestWorkspaceInvite_SubjectIsRFC2047Encoded(t *testing.T) {
	r, sender := newInviteRouter(t)
	want := map[string]string{
		"en": "You have been invited to a workspace on Markerloop",
		"ru": "Приглашение в рабочее пространство Markerloop",
		"kk": "Markerloop жұмыс кеңістігіне шақыру",
	}
	for i, locale := range []string{"en", "ru", "kk"} {
		t.Run(locale, func(t *testing.T) {
			w := postInvite(t, r, inviteKey, invitePayload(locale))
			if w.Code != http.StatusOK {
				t.Fatalf("ожидался 200, получен %d: %s", w.Code, w.Body.String())
			}
			sender.mu.Lock()
			defer sender.mu.Unlock()
			raw := sender.sent[i].Subject
			for _, b := range []byte(raw) {
				if b < 0x20 || b > 0x7e {
					t.Fatalf("в сыром Subject не-ASCII байт 0x%02x: %q", b, raw)
				}
			}
			decoded, err := new(mime.WordDecoder).DecodeHeader(raw)
			if err != nil {
				t.Fatalf("Subject не декодируется по RFC 2047: %v: %q", err, raw)
			}
			if decoded != want[locale] {
				t.Errorf("Subject после декодирования %q, ожидалось %q", decoded, want[locale])
			}
		})
	}
}

func TestWorkspaceInvite_EndToEndRendersEscapedMail(t *testing.T) {
	r, sender := newInviteRouter(t)
	for i, locale := range []string{"ru", "en", "kk"} {
		t.Run(locale, func(t *testing.T) {
			w := postInvite(t, r, inviteKey, invitePayload(locale))
			if w.Code != http.StatusOK {
				t.Fatalf("ожидался 200, получен %d: %s", w.Code, w.Body.String())
			}
			var resp struct {
				Success bool `json:"success"`
				Data    struct {
					To string `json:"to"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("ответ не JSON: %v: %s", err, w.Body.String())
			}
			if !resp.Success || resp.Data.To != "invitee@example.com" {
				t.Fatalf("неожиданный ответ: %s", w.Body.String())
			}

			sender.mu.Lock()
			defer sender.mu.Unlock()
			if len(sender.sent) != i+1 {
				t.Fatalf("ожидалось %d писем, перехвачено %d", i+1, len(sender.sent))
			}
			mail := sender.sent[i]
			if mail.To != "invitee@example.com" {
				t.Errorf("письмо ушло не туда: %q", mail.To)
			}
			for _, raw := range []string{"<script>", `<a href="https://evil.test">`} {
				if strings.Contains(mail.Body, raw) {
					t.Errorf("сырой HTML из имени попал в письмо: %q", raw)
				}
			}
			if !strings.Contains(mail.Body, "&lt;script&gt;") {
				t.Errorf("имя пространства не экранировано")
			}
			for _, frag := range []string{"Acme", "Bob", "<", "mli_"} {
				if strings.Contains(mail.Subject, frag) {
					t.Errorf("тема содержит пользовательские данные: %q", mail.Subject)
				}
			}
			t.Logf("=== %s subject: %s", locale, mail.Subject)
			t.Logf("=== %s body:\n%s", locale, mail.Body)
		})
	}
}
