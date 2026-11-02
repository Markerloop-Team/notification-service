package services

import (
	"context"

	"github.com/sirupsen/logrus"

	"notification-service/internal/config"
	"notification-service/internal/telegram"
)

// TelegramService сервис для работы с Telegram уведомлениями
type TelegramService struct {
	sender telegram.TelegramSender
	logger *logrus.Logger
}

// NewTelegramService создаёт новый Telegram сервис
func NewTelegramService(cfg config.TelegramConfig, logger *logrus.Logger) *TelegramService {
	return &TelegramService{
		sender: telegram.NewBotAdapter(cfg.BotServiceURL, cfg.ServiceKey, cfg.Timeout, cfg.RetryAttempts, cfg.RetryBackoff, logger),
		logger: logger,
	}
}

// SendMessage отправляет сообщение пользователю
func (s *TelegramService) SendMessage(ctx context.Context, userID, title, message string) error {
	return s.sender.SendMessage(ctx, userID, title, message)
}

// SendDeployNotificationPayload пересылает rich-payload о деплое в бот.
func (s *TelegramService) SendDeployNotificationPayload(ctx context.Context, payload telegram.DeployPayload) error {
	return s.sender.SendDeployNotificationPayload(ctx, payload)
}
