package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"notification-service/internal/config"
	"notification-service/internal/email"
	"notification-service/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	sdklogger "gitlab.com/markerloop/go-sdk/pkg/logger"
	"gitlab.com/markerloop/go-sdk/pkg/requestid"
)

// SDL-738: журналы notification-service не несут адрес получателя — только
// request_id и тип письма. Тест гоняет отправку через хендлеры на логгере
// уровня Debug с перехватом вывода; отправитель-заглушка в одном из сценариев
// возвращает ошибку, эхом несущую адрес (как ответ SMTP на RCPT TO).

const probeAddr = "leak-probe@example.org"

type echoSender struct{ fail bool }

func (s echoSender) SendEmail(_ context.Context, to, _, _ string) error {
	if s.fail {
		return fmt.Errorf("ошибка RCPT TO: 550 5.1.1 <%s>: Recipient address rejected", to)
	}
	return nil
}

func captureLogger() (*logrus.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	l := logrus.New()
	l.SetOutput(buf)
	l.SetLevel(logrus.DebugLevel)
	l.SetFormatter(&logrus.JSONFormatter{})
	l.AddHook(&sdklogger.RequestIDHook{})
	return l, buf
}

func routerFor(sender email.EmailSender, logger *logrus.Logger) *gin.Engine {
	gin.SetMode(gin.TestMode)
	svc := services.NewNotificationServiceWithSender(sender, config.TelegramConfig{}, config.EmailTemplatesConfig{
		VerificationSubject: "verify", WelcomeSubject: "welcome", PasswordResetSubject: "reset",
	}, logger)
	h := NewNotificationHandler(*svc, logger)
	r := gin.New()
	r.Use(requestid.Middleware())
	r.POST("/send", h.SendEmail)
	r.POST("/send-verification", h.SendVerificationEmail)
	r.POST("/send-workspace-invite", h.SendWorkspaceInviteEmail)
	return r
}

func TestNoRecipientInLogs(t *testing.T) {
	bodies := map[string]string{
		"/send":              `{"to":"` + probeAddr + `","subject":"s","body":"b"}`,
		"/send-verification": `{"to":"` + probeAddr + `","code":"123456"}`,
		"/send-workspace-invite": `{"to":"` + probeAddr + `","workspace_name":"W","inviter_name":"I",` +
			`"accept_url":"https://admin.markerloop.com/invite?token=t","expires_at":"2026-10-15T00:00:00Z"}`,
	}
	for _, fail := range []bool{false, true} {
		logger, buf := captureLogger()
		r := routerFor(echoSender{fail: fail}, logger)
		for path, body := range bodies {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(requestid.Header, "sdl738-probe")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if !fail && w.Code != http.StatusOK {
				t.Fatalf("%s: статус %d, тело %s", path, w.Code, w.Body.String())
			}
			if fail && strings.Contains(w.Body.String(), probeAddr) {
				t.Fatalf("%s: адрес вернулся в теле ответа: %s", path, w.Body.String())
			}
		}
		out := buf.String()
		if out == "" {
			t.Fatal("логгер ничего не записал — тест перестал что-либо проверять")
		}
		if strings.Contains(out, probeAddr) {
			t.Fatalf("fail=%v: лог содержит адрес получателя:\n%s", fail, out)
		}
		if strings.Contains(out, "123456") {
			t.Fatalf("fail=%v: лог содержит код верификации:\n%s", fail, out)
		}
		if !strings.Contains(out, "sdl738-probe") {
			t.Fatalf("fail=%v: в логе нет request_id:\n%s", fail, out)
		}
	}
}

// TestNoRecipientInAdapterLogs — адаптеры SMTP пишут шапку отправки до
// подключения; порт закрыт, поэтому отправка падает сразу, а шапка уже в логе.
func TestNoRecipientInAdapterLogs(t *testing.T) {
	logger, buf := captureLogger()
	senders := []email.EmailSender{
		email.NewGoogleSMTPAdapter("127.0.0.1", 1, "", "", "noreply@markerloop.com", 200*time.Millisecond, logger),
		email.NewMailHogAdapter("127.0.0.1", 1, "noreply@markerloop.com", 200*time.Millisecond, logger),
	}
	for _, s := range senders {
		_ = s.SendEmail(context.Background(), probeAddr, "s", "b")
	}
	if buf.Len() == 0 {
		t.Fatal("логгер ничего не записал")
	}
	if strings.Contains(buf.String(), probeAddr) {
		t.Fatalf("лог адаптера содержит адрес получателя:\n%s", buf.String())
	}
}
