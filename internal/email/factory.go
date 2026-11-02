package email

import (
	"notification-service/internal/config"

	"github.com/sirupsen/logrus"
)

// NewEmailSender создает соответствующий адаптер для отправки email
// на основе конфигурации. Использует паттерн Factory для выбора правильной реализации.
func NewEmailSender(cfg config.EmailConfig, logger *logrus.Logger) EmailSender {
	host := cfg.Host
	port := cfg.Port

	// Определяем, является ли это MailHog (локальная разработка)
	isMailHog := (host == "localhost" || host == "127.0.0.1") && port == 1025
	// Если явно указано отключить TLS, считаем это MailHog
	if cfg.DisableTLS {
		isMailHog = true
	}

	if isMailHog {
		logger.Infof("🔧 Использование MailHog адаптера для локальной разработки")
		return NewMailHogAdapter(
			cfg.Host,
			cfg.Port,
			cfg.From,
			cfg.Timeout,
			logger,
		)
	}

	// Для всех остальных случаев используем Google SMTP адаптер
	logger.Infof("🔧 Использование Google SMTP адаптера для production")
	return NewGoogleSMTPAdapter(
		cfg.Host,
		cfg.Port,
		cfg.User,
		cfg.Password,
		cfg.From,
		cfg.Timeout,
		logger,
	)
}
