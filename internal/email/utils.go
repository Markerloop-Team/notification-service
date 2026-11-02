package email

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// buildMessage формирует email сообщение с заголовками
func buildMessage(from, to, subject, body, messageID string) string {
	headers := []string{
		fmt.Sprintf("From: %s", from),
		fmt.Sprintf("To: %s", to),
		fmt.Sprintf("Subject: %s", subject),
		fmt.Sprintf("Message-ID: %s", messageID),
		fmt.Sprintf("Date: %s", time.Now().Format(time.RFC1123Z)),
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"X-Mailer: Markerloop Notification Service",
	}

	return strings.Join(headers, "\r\n") + "\r\n\r\n" + body
}

// generateMessageID генерирует уникальный Message-ID для email
func generateMessageID(fromEmail string) string {
	domain := getDomainFromEmail(fromEmail)
	return fmt.Sprintf("<%s@%s>", uuid.New().String(), domain)
}

// getDomainFromEmail извлекает домен из email адреса
func getDomainFromEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) == 2 {
		return parts[1]
	}
	return "markerloop.com"
}

// redactedError — ошибка отправки, из текста которой вырезан адрес
// получателя (SDL-738). Причина доступна через Unwrap.
type redactedError struct {
	msg   string
	cause error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.cause }

// RedactRecipient убирает адрес получателя из текста ошибки: ответ SMTP на
// RCPT TO и обёртки вокруг него могут эхом нести адрес, а ошибка уходит в
// журнал. Персональные данные в логи не пишем (SDL-738).
//
// Поиск без учёта регистра: сервер может вернуть адрес в другом регистре.
func RedactRecipient(err error, to string) error {
	if err == nil || to == "" {
		return err
	}
	re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(to))
	msg := err.Error()
	if !re.MatchString(msg) {
		return err
	}
	return &redactedError{msg: re.ReplaceAllLiteralString(msg, "<recipient>"), cause: err}
}
