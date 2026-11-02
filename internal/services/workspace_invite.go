package services

import (
	"bytes"
	"fmt"
	"html/template"
	"mime"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"notification-service/internal/models"
)

// SDL-615 (S3): письмо-приглашение в рабочее пространство.
//
// Отличие от соседних писем (verification/welcome/password-reset): в тело
// попадают строки, которые ввёл ПОЛЬЗОВАТЕЛЬ — имя пространства и имя
// приглашающего. Соседи собирают HTML через fmt.Sprintf, и для них это
// терпимо (код, URL из конфига, имя из профиля), а здесь fmt.Sprintf означал
// бы HTML-инъекцию в почтовый клиент получателя. Поэтому шаблон — на
// html/template: он экранирует по контексту (текст, атрибут href), а не по
// доброй воле автора. Общий buildMessage не трогаем (план, §9 п.13а).
//
// Тема письма фиксирована на локаль и не содержит пользовательских данных:
// заголовок Subject уходит в SMTP сырым, экранировать его нечем.

// inviteMaxNameRunes — предел длины имени пространства и приглашающего в
// письме (план, §4). Обрезаем по рунам, чтобы не порвать UTF-8.
const inviteMaxNameRunes = 80

// inviteDefaultLocale — локаль по умолчанию, когда пришла пустая или
// неизвестная.
const inviteDefaultLocale = "en"

// inviteStrings — строки письма для одной локали. Никакой пользовательский
// ввод сюда не попадает: это константы.
type inviteStrings struct {
	Subject    string
	Title      string
	Greeting   string
	InvitedBy  string
	Intro      string
	Button     string
	FallbackTo string
	ExpiresOn  string
	Ignore     string
	Signature  string
}

// inviteLocales — ru/en/kk. Порядок ключей не важен: выбор идёт по коду.
var inviteLocales = map[string]inviteStrings{
	"en": {
		Subject:    "You have been invited to a workspace on Markerloop",
		Title:      "Workspace invitation",
		Greeting:   "Hello!",
		InvitedBy:  "invited you to join the workspace",
		Intro:      "Accept the invitation to start working together.",
		Button:     "Join workspace",
		FallbackTo: "If the button does not work, copy and paste this link into your browser:",
		ExpiresOn:  "This invitation is valid until",
		Ignore:     "If you did not expect this invitation, simply ignore this email.",
		Signature:  "Best regards,",
	},
	"ru": {
		Subject:    "Приглашение в рабочее пространство Markerloop",
		Title:      "Приглашение в рабочее пространство",
		Greeting:   "Здравствуйте!",
		InvitedBy:  "приглашает вас в рабочее пространство",
		Intro:      "Примите приглашение, чтобы начать работать вместе.",
		Button:     "Присоединиться",
		FallbackTo: "Если кнопка не работает, скопируйте и вставьте эту ссылку в браузер:",
		ExpiresOn:  "Приглашение действительно до",
		Ignore:     "Если вы не ожидали этого приглашения, просто проигнорируйте письмо.",
		Signature:  "С уважением,",
	},
	"kk": {
		Subject:    "Markerloop жұмыс кеңістігіне шақыру",
		Title:      "Жұмыс кеңістігіне шақыру",
		Greeting:   "Сәлеметсіз бе!",
		InvitedBy:  "сізді жұмыс кеңістігіне шақырады",
		Intro:      "Бірге жұмыс істеуді бастау үшін шақыруды қабылдаңыз.",
		Button:     "Қосылу",
		FallbackTo: "Түйме жұмыс істемесе, мына сілтемені браузерге көшіріп қойыңыз:",
		ExpiresOn:  "Шақыру мына күнге дейін жарамды:",
		Ignore:     "Бұл шақыруды күтпеген болсаңыз, хатты елемей қойыңыз.",
		Signature:  "Құрметпен,",
	},
}

// inviteTemplateData — данные шаблона. Имена — сырые строки: их экранирует
// html/template по месту подстановки.
type inviteTemplateData struct {
	Lang          string
	S             inviteStrings
	WorkspaceName string
	InviterName   string
	AcceptURL     string
	ExpiresAt     string
	CompanyName   string
}

// inviteTemplate разбирается один раз при загрузке пакета: ошибка в разметке
// уронит процесс на старте, а не первое письмо в проде.
var inviteTemplate = template.Must(template.New("workspace-invite").Parse(`<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<meta http-equiv="X-UA-Compatible" content="IE=edge">
	<title>{{.S.Title}}</title>
</head>
<body style="margin: 0; padding: 0; background-color: #f4f4f4; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;">
	<table role="presentation" style="width: 100%; border-collapse: collapse; background-color: #f4f4f4;">
		<tr>
			<td style="padding: 40px 20px;">
				<table role="presentation" style="max-width: 600px; margin: 0 auto; background-color: #ffffff; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1);">
					<tr>
						<td style="padding: 40px 40px 30px; text-align: center; background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); border-radius: 8px 8px 0 0;">
							<h1 style="margin: 0; color: #ffffff; font-size: 28px; font-weight: 600; letter-spacing: -0.5px;">{{.S.Title}}</h1>
						</td>
					</tr>
					<tr>
						<td style="padding: 40px 40px 20px;">
							<p style="margin: 0 0 20px; color: #333333; font-size: 16px; line-height: 1.6;">{{.S.Greeting}}</p>
							<p style="margin: 0 0 30px; color: #666666; font-size: 16px; line-height: 1.6;"><strong style="color: #333333;">{{.InviterName}}</strong> {{.S.InvitedBy}} <strong style="color: #333333;">{{.WorkspaceName}}</strong>. {{.S.Intro}}</p>
							<div style="text-align: center; margin: 40px 0;">
								<a href="{{.AcceptURL}}" style="display: inline-block; background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); color: #ffffff; text-decoration: none; padding: 16px 40px; border-radius: 6px; font-weight: 600; font-size: 16px; box-shadow: 0 4px 6px rgba(102, 126, 234, 0.3);">{{.S.Button}}</a>
							</div>
							<p style="margin: 30px 0 20px; color: #999999; font-size: 14px; line-height: 1.5;">{{.S.FallbackTo}}</p>
							<div style="background-color: #f8f9fa; padding: 15px; border-radius: 6px; border: 1px solid #e9ecef; margin-bottom: 30px;">
								<p style="margin: 0; word-break: break-all; color: #495057; font-size: 13px; line-height: 1.6; font-family: 'Courier New', monospace;">{{.AcceptURL}}</p>
							</div>
							<div style="background-color: #fff3cd; border-left: 4px solid #ffc107; padding: 15px 20px; margin: 30px 0; border-radius: 4px;">
								<p style="margin: 0; color: #856404; font-size: 14px; line-height: 1.5;">{{.S.ExpiresOn}} <strong>{{.ExpiresAt}}</strong>.</p>
							</div>
							<p style="margin: 30px 0 0; color: #999999; font-size: 14px; line-height: 1.5;">{{.S.Ignore}}</p>
						</td>
					</tr>
					<tr>
						<td style="padding: 30px 40px; background-color: #f8f9fa; border-radius: 0 0 8px 8px; border-top: 1px solid #e9ecef;">
							<p style="margin: 0; color: #6c757d; font-size: 13px; line-height: 1.5; text-align: center;">
								{{.S.Signature}}<br>
								<strong style="color: #495057;">{{.CompanyName}}</strong>
							</p>
						</td>
					</tr>
				</table>
			</td>
		</tr>
	</table>
</body>
</html>
`))

// resolveInviteLocale отдаёт строки локали; пустая или неизвестная → en.
func resolveInviteLocale(locale string) (string, inviteStrings) {
	code := strings.ToLower(strings.TrimSpace(locale))
	if s, ok := inviteLocales[code]; ok {
		return code, s
	}
	return inviteDefaultLocale, inviteLocales[inviteDefaultLocale]
}

// truncateRunes обрезает строку до max рун.
func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}

// validateAcceptURL принимает только абсолютный https-адрес без userinfo.
// Оба контура строят ссылку из INVITE_BASE_URL вида https://… (план, §4),
// http не нужен никому. html/template и так превратит `javascript:` в
// `#ZgotmplZ`, но тихая порча ссылки в письме хуже честного 400 вызывающему
// сервису. Текст ошибки НЕ содержит входа: в accept_url лежит токен.
func validateAcceptURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("accept_url не разбирается как URL")
	}
	if u.Scheme != "https" {
		return fmt.Errorf("accept_url должен использовать схему https")
	}
	if u.Host == "" {
		return fmt.Errorf("accept_url должен быть абсолютным адресом с хостом")
	}
	if u.User != nil {
		return fmt.Errorf("accept_url не должен содержать userinfo")
	}
	return nil
}

// renderWorkspaceInvite собирает тему и HTML письма. Вынесено из
// SendWorkspaceInviteEmail, чтобы тесты проверяли экранирование без SMTP.
func (s *NotificationService) renderWorkspaceInvite(req *models.WorkspaceInviteEmailRequest) (subject, body string, err error) {
	if err := validateAcceptURL(req.AcceptURL); err != nil {
		return "", "", err
	}
	expiresAt, err := time.Parse(time.RFC3339, req.ExpiresAt)
	if err != nil {
		return "", "", fmt.Errorf("expires_at не в формате RFC3339")
	}

	lang, strs := resolveInviteLocale(req.Locale)
	data := inviteTemplateData{
		Lang:          lang,
		S:             strs,
		WorkspaceName: truncateRunes(req.WorkspaceName, inviteMaxNameRunes),
		InviterName:   truncateRunes(req.InviterName, inviteMaxNameRunes),
		AcceptURL:     req.AcceptURL,
		ExpiresAt:     expiresAt.UTC().Format("2006-01-02 15:04 UTC"),
		CompanyName:   s.emailTemplates.CompanyName,
	}

	var buf bytes.Buffer
	if err := inviteTemplate.Execute(&buf, data); err != nil {
		return "", "", fmt.Errorf("рендер шаблона приглашения: %w", err)
	}
	return strs.Subject, buf.String(), nil
}

// SendWorkspaceInviteEmail отправляет письмо-приглашение в рабочее
// пространство. Ошибка валидации входа (URL, дата) возвращается как
// *InviteValidationError, чтобы хендлер ответил 400, а не 500.
func (s *NotificationService) SendWorkspaceInviteEmail(req *models.WorkspaceInviteEmailRequest) (*models.EmailResponse, error) {
	subject, body, err := s.renderWorkspaceInvite(req)
	if err != nil {
		return nil, &InviteValidationError{Err: err}
	}

	// Subject уходит в заголовок сырым (buildMessage); Q-кодирование делает
	// нелатинскую тему корректной по RFC 2047, ASCII-строку оставляет как есть.
	emailReq := &models.EmailRequest{
		To:      req.To,
		Subject: mime.QEncoding.Encode("utf-8", subject),
		Body:    body,
	}
	return s.SendEmail(emailReq)
}

// InviteValidationError — вход не прошёл проверку, письмо не отправлялось.
type InviteValidationError struct{ Err error }

func (e *InviteValidationError) Error() string { return e.Err.Error() }
func (e *InviteValidationError) Unwrap() error { return e.Err }
