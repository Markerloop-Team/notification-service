package email

import (
	"context"
)

// EmailSender интерфейс для отправки email
// Реализуется различными адаптерами (MailHog, Google SMTP и т.д.)
type EmailSender interface {
	// SendEmail отправляет email сообщение
	SendEmail(ctx context.Context, to, subject, body string) error
}
