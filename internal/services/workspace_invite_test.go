package services

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"notification-service/internal/config"
	"notification-service/internal/models"
)

// SDL-615 (S3): письмо-приглашение. Имя пространства и приглашающего —
// пользовательский ввод; тесты стерегут, что шаблон экранирует его САМ, а
// тема письма его не содержит вовсе. Подсадка «fmt.Sprintf вместо шаблона»
// обязана красить TestWorkspaceInvite_EscapesUserSuppliedNames.

const (
	hostileWorkspace = `<script>alert("ws")</script> Acme & Co`
	hostileInviter   = `<a href="https://evil.test">click</a> Bob`
	validAcceptURL   = "https://test.markerloop.com/invite#token=mli_abc123"
	validExpiresAt   = "2026-10-04T12:34:56Z"
)

func newRenderService(t *testing.T) *NotificationService {
	t.Helper()
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return NewNotificationServiceWithSender(nil, config.TelegramConfig{}, config.EmailTemplatesConfig{CompanyName: "Команда MarkerLoop"}, logger)
}

func inviteReq(locale string) *models.WorkspaceInviteEmailRequest {
	return &models.WorkspaceInviteEmailRequest{
		To:            "invitee@example.com",
		WorkspaceName: hostileWorkspace,
		InviterName:   hostileInviter,
		AcceptURL:     validAcceptURL,
		ExpiresAt:     validExpiresAt,
		Locale:        locale,
	}
}

func TestWorkspaceInvite_EscapesUserSuppliedNames(t *testing.T) {
	svc := newRenderService(t)
	for _, locale := range []string{"ru", "en", "kk"} {
		t.Run(locale, func(t *testing.T) {
			_, body, err := svc.renderWorkspaceInvite(inviteReq(locale))
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			for _, raw := range []string{"<script>", "</script>", `<a href="https://evil.test">`, "click</a>"} {
				if strings.Contains(body, raw) {
					t.Errorf("сырой пользовательский HTML %q попал в письмо", raw)
				}
			}
			for _, escaped := range []string{"&lt;script&gt;", "&lt;a href=", "Acme &amp; Co"} {
				if !strings.Contains(body, escaped) {
					t.Errorf("ожидалась экранированная форма %q, в письме её нет", escaped)
				}
			}
			// Единственные <a> в письме — наши: ссылка принятия.
			if got := strings.Count(body, "<a "); got != 1 {
				t.Errorf("в письме должен быть ровно один тег <a> (кнопка), найдено %d", got)
			}
			if !strings.Contains(body, `href="`+validAcceptURL+`"`) {
				t.Errorf("ссылка принятия не попала в href как есть")
			}
		})
	}
}

func TestWorkspaceInvite_SubjectCarriesNoUserData(t *testing.T) {
	svc := newRenderService(t)
	for _, locale := range []string{"ru", "en", "kk"} {
		t.Run(locale, func(t *testing.T) {
			subject, _, err := svc.renderWorkspaceInvite(inviteReq(locale))
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if subject != inviteLocales[locale].Subject {
				t.Errorf("тема должна быть константой локали %q, получена %q", inviteLocales[locale].Subject, subject)
			}
			for _, frag := range []string{"Acme", "Bob", "<", "evil.test", "mli_"} {
				if strings.Contains(subject, frag) {
					t.Errorf("в теме письма пользовательские данные: %q содержит %q", subject, frag)
				}
			}
		})
	}
}

func TestWorkspaceInvite_LocaleFallsBackToEnglish(t *testing.T) {
	svc := newRenderService(t)
	for _, locale := range []string{"", "de", "RU ", "En"} {
		subject, body, err := svc.renderWorkspaceInvite(inviteReq(locale))
		if err != nil {
			t.Fatalf("locale %q: %v", locale, err)
		}
		want := "en"
		switch strings.ToLower(strings.TrimSpace(locale)) {
		case "ru", "kk":
			want = strings.ToLower(strings.TrimSpace(locale))
		}
		if subject != inviteLocales[want].Subject {
			t.Errorf("locale %q: ожидалась тема %q, получена %q", locale, inviteLocales[want].Subject, subject)
		}
		if !strings.Contains(body, `<html lang="`+want+`">`) {
			t.Errorf("locale %q: lang в разметке не %q", locale, want)
		}
	}
}

func TestWorkspaceInvite_RejectsBadInput(t *testing.T) {
	svc := newRenderService(t)
	cases := map[string]func(*models.WorkspaceInviteEmailRequest){
		"javascript-url":  func(r *models.WorkspaceInviteEmailRequest) { r.AcceptURL = "javascript:alert(1)" },
		"javascript-host": func(r *models.WorkspaceInviteEmailRequest) { r.AcceptURL = "javascript://evil.test/%0aalert(1)" },
		"ftp-url":         func(r *models.WorkspaceInviteEmailRequest) { r.AcceptURL = "ftp://x/invite" },
		"http-url":        func(r *models.WorkspaceInviteEmailRequest) { r.AcceptURL = "http://test.markerloop.com/invite#token=x" },
		"userinfo-url": func(r *models.WorkspaceInviteEmailRequest) {
			r.AcceptURL = "https://markerloop.com@evil.test/invite#token=x"
		},
		"relative-url":    func(r *models.WorkspaceInviteEmailRequest) { r.AcceptURL = "/invite#token=x" },
		"no-host-url":     func(r *models.WorkspaceInviteEmailRequest) { r.AcceptURL = "https:///invite" },
		"bad-expires-at":  func(r *models.WorkspaceInviteEmailRequest) { r.ExpiresAt = "next week" },
		"expires-not-utc": func(r *models.WorkspaceInviteEmailRequest) { r.ExpiresAt = "2026-10-04 12:34" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := inviteReq("en")
			mutate(r)
			_, _, err := svc.renderWorkspaceInvite(r)
			if err == nil {
				t.Fatalf("ожидалась ошибка валидации, письмо отрендерилось")
			}
			// В accept_url лежит токен: текст ошибки не должен его повторять.
			for _, leak := range []string{"token=", "evil.test", "alert(", "ftp://"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("текст ошибки повторяет вход (%q): %s", leak, err)
				}
			}
			_, err = svc.SendWorkspaceInviteEmail(r)
			var ve *InviteValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("SendWorkspaceInviteEmail: ожидался *InviteValidationError, получено %T %v", err, err)
			}
		})
	}
}

func TestWorkspaceInvite_TruncatesLongNames(t *testing.T) {
	svc := newRenderService(t)
	r := inviteReq("en")
	r.WorkspaceName = strings.Repeat("я", 200)
	_, body, err := svc.renderWorkspaceInvite(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, strings.Repeat("я", 81)) {
		t.Error("имя пространства не обрезано до 80 рун")
	}
	if !strings.Contains(body, strings.Repeat("я", 80)+"…") {
		t.Error("обрезанное имя должно заканчиваться многоточием")
	}
}

func TestWorkspaceInvite_ExpiresAtRenderedInUTC(t *testing.T) {
	svc := newRenderService(t)
	r := inviteReq("en")
	r.ExpiresAt = "2026-10-04T17:34:56+05:00"
	_, body, err := svc.renderWorkspaceInvite(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "2026-10-04 12:34 UTC") {
		t.Error("expires_at должен быть приведён к UTC в письме")
	}
}
