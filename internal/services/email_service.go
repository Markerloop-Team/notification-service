package services

import (
	"context"
	"time"

	"notification-service/internal/config"
	"notification-service/internal/email"

	"github.com/sirupsen/logrus"
)

// EmailService сервис для отправки email
// Использует паттерн адаптера для работы с разными SMTP провайдерами
type EmailService struct {
	emailSender email.EmailSender
	logger      *logrus.Logger
	timeout     time.Duration
}

// NewEmailService создает новый email сервис
// Автоматически выбирает правильный адаптер на основе конфигурации
func NewEmailService(cfg config.EmailConfig, logger *logrus.Logger) *EmailService {
	return &EmailService{
		emailSender: email.NewEmailSender(cfg, logger),
		logger:      logger,
		timeout:     cfg.Timeout,
	}
}

// SendEmail отправляет email с поддержкой контекста
func (s *EmailService) SendEmail(ctx context.Context, to, subject, body string) error {
	// Создаем контекст с timeout для гарантированного прерывания операции
	emailCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	return email.RedactRecipient(s.emailSender.SendEmail(emailCtx, to, subject, body), to)
}
