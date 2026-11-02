package services

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"notification-service/internal/config"
	"notification-service/internal/email"
	"notification-service/internal/models"
	"notification-service/internal/telegram"
)

// NotificationService сервис для работы с уведомлениями
type NotificationService struct {
	emailService    *EmailService
	telegramService *TelegramService
	logger          *logrus.Logger
	emailTemplates  config.EmailTemplatesConfig
}

// NewNotificationService создает новый сервис уведомлений
func NewNotificationService(cfg config.EmailConfig, telegramCfg config.TelegramConfig, emailTemplates config.EmailTemplatesConfig, logger *logrus.Logger) *NotificationService {
	return &NotificationService{
		emailService:    NewEmailService(cfg, logger),
		telegramService: NewTelegramService(telegramCfg, logger),
		logger:          logger,
		emailTemplates:  emailTemplates,
	}
}

// NewNotificationServiceWithSender собирает сервис поверх готового
// email.EmailSender. Нужен тестам (SDL-615): прогнать НАСТОЯЩИЙ хендлер и
// роутер до самого письма, перехватив его вместо SMTP. Telegram-канал
// собирается как обычно.
func NewNotificationServiceWithSender(sender email.EmailSender, telegramCfg config.TelegramConfig, emailTemplates config.EmailTemplatesConfig, logger *logrus.Logger) *NotificationService {
	return &NotificationService{
		emailService:    &EmailService{emailSender: sender, logger: logger, timeout: 5 * time.Second},
		telegramService: NewTelegramService(telegramCfg, logger),
		logger:          logger,
		emailTemplates:  emailTemplates,
	}
}

// SendTelegramMessage отправляет Telegram сообщение пользователю
func (s *NotificationService) SendTelegramMessage(req *models.TelegramSendRequest) (*models.TelegramSendResponse, error) {
	ctx := context.Background()
	if err := s.telegramService.SendMessage(ctx, req.UserID, req.Title, req.Message); err != nil {
		return nil, fmt.Errorf("ошибка отправки Telegram: %w", err)
	}
	s.logger.Infof("Telegram сообщение отправлено пользователю %s", req.UserID)
	return &models.TelegramSendResponse{
		Message: "Telegram уведомление отправлено",
		UserID:  req.UserID,
	}, nil
}

// SendDeployNotification пересылает rich-уведомление о деплое в бот.
// Все поля кроме App опциональны — пустые просто опускаются в JSON
// (omitempty), и бот форматирует то, что пришло, без лишней магии.
func (s *NotificationService) SendDeployNotification(req *models.DeployNotificationRequest) (*models.DeployNotificationResponse, error) {
	ctx := context.Background()
	payload := telegram.DeployPayload{
		App:          req.App,
		Status:       req.Status,
		Revision:     req.Revision,
		EventType:    req.EventType,
		RevisionURL:  req.RevisionURL,
		ArgocdURL:    req.ArgocdURL,
		RepoURL:      req.RepoURL,
		StartedAt:    req.StartedAt,
		FinishedAt:   req.FinishedAt,
		Message:      req.Message,
		HealthStatus: req.HealthStatus,
		SyncStatus:   req.SyncStatus,
	}
	if err := s.telegramService.SendDeployNotificationPayload(ctx, payload); err != nil {
		return nil, fmt.Errorf("ошибка отправки deploy notification: %w", err)
	}
	s.logger.Infof(
		"Deploy notification отправлен: app=%s event=%s status=%s",
		req.App, req.EventType, req.Status,
	)
	return &models.DeployNotificationResponse{
		Message: "Deploy notification переслан в бот",
		App:     req.App,
	}, nil
}

// SendEmail отправляет обычный email
func (s *NotificationService) SendEmail(req *models.EmailRequest) (*models.EmailResponse, error) {
	s.logger.Debug("NotificationService.SendEmail: начало отправки email")
	s.logger.Debugf("NotificationService.SendEmail: тема - %s", req.Subject)

	ctx := context.Background()
	s.logger.Debug("NotificationService.SendEmail: вызов emailService.SendEmail...")
	if err := s.emailService.SendEmail(ctx, req.To, req.Subject, req.Body); err != nil {
		s.logger.Errorf("NotificationService.SendEmail: ошибка отправки email: %v", err)
		return nil, fmt.Errorf("ошибка отправки email: %w", err)
	}

	s.logger.Info("NotificationService.SendEmail: письмо принято к доставке (SMTP 250 — приём, не доставка)")
	return &models.EmailResponse{
		Message: "Письмо принято к доставке",
		To:      req.To,
	}, nil
}

// SendVerificationEmail отправляет email верификации
func (s *NotificationService) SendVerificationEmail(req *models.VerificationEmailRequest) (*models.EmailResponse, error) {
	s.logger.Debug("NotificationService.SendVerificationEmail: начало отправки email верификации")

	// Генерируем данные для универсального метода
	emailReq := &models.EmailRequest{
		To:      req.To,
		Subject: s.emailTemplates.VerificationSubject,
		Body:    s.generateVerificationTemplate(req.Code),
	}

	s.logger.Debug("NotificationService.SendVerificationEmail: вызов SendEmail...")
	// Переиспользуем основной метод
	return s.SendEmail(emailReq)
}

// generateVerificationTemplate генерирует HTML шаблон для email верификации
func (s *NotificationService) generateVerificationTemplate(code string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<meta http-equiv="X-UA-Compatible" content="IE=edge">
	<title>Подтверждение email адреса</title>
</head>
<body style="margin: 0; padding: 0; background-color: #f4f4f4; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;">
	<table role="presentation" style="width: 100%%; border-collapse: collapse; background-color: #f4f4f4;">
		<tr>
			<td style="padding: 40px 20px;">
				<table role="presentation" style="max-width: 600px; margin: 0 auto; background-color: #ffffff; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1);">
					<tr>
						<td style="padding: 40px 40px 30px; text-align: center; background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%); border-radius: 8px 8px 0 0;">
							<h1 style="margin: 0; color: #ffffff; font-size: 28px; font-weight: 600; letter-spacing: -0.5px;">Подтверждение email</h1>
						</td>
					</tr>
					<tr>
						<td style="padding: 40px 40px 20px;">
							<p style="margin: 0 0 20px; color: #333333; font-size: 16px; line-height: 1.6;">Здравствуйте!</p>
							<p style="margin: 0 0 30px; color: #666666; font-size: 16px; line-height: 1.6;">Для подтверждения вашего email адреса используйте следующий код верификации:</p>
							<div style="background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%); padding: 30px; text-align: center; border-radius: 8px; margin: 30px 0;">
								<div style="background-color: #ffffff; padding: 20px; border-radius: 6px; display: inline-block; min-width: 200px;">
									<h2 style="margin: 0; color: #667eea; font-size: 36px; font-weight: 700; letter-spacing: 8px; font-family: 'Courier New', monospace;">%s</h2>
								</div>
							</div>
							<div style="background-color: #fff3cd; border-left: 4px solid #ffc107; padding: 15px 20px; margin: 30px 0; border-radius: 4px;">
								<p style="margin: 0; color: #856404; font-size: 14px; line-height: 1.5;">
									<strong>⏱ Важно:</strong> Этот код действителен в течение <strong>%s</strong>. После истечения срока действия вам потребуется запросить новый код.
								</p>
							</div>
							<p style="margin: 30px 0 0; color: #999999; font-size: 14px; line-height: 1.5;">Если вы не регистрировались в нашем сервисе, просто проигнорируйте это письмо.</p>
						</td>
					</tr>
					<tr>
						<td style="padding: 30px 40px; background-color: #f8f9fa; border-radius: 0 0 8px 8px; border-top: 1px solid #e9ecef;">
							<p style="margin: 0; color: #6c757d; font-size: 13px; line-height: 1.5; text-align: center;">
								С уважением,<br>
								<strong style="color: #495057;">%s</strong>
							</p>
						</td>
					</tr>
				</table>
			</td>
		</tr>
	</table>
</body>
</html>`, code, s.emailTemplates.VerificationCodeValidity, s.emailTemplates.CompanyName)
}

// SendWelcomeEmail отправляет приветственный email
func (s *NotificationService) SendWelcomeEmail(req *models.WelcomeEmailRequest) (*models.EmailResponse, error) {
	emailReq := &models.EmailRequest{
		To:      req.To,
		Subject: s.emailTemplates.WelcomeSubject,
		Body:    s.generateWelcomeTemplate(req.UserName),
	}

	return s.SendEmail(emailReq)
}

// SendPasswordResetEmail отправляет email сброса пароля
func (s *NotificationService) SendPasswordResetEmail(req *models.PasswordResetEmailRequest) (*models.EmailResponse, error) {
	emailReq := &models.EmailRequest{
		To:      req.To,
		Subject: s.emailTemplates.PasswordResetSubject,
		Body:    s.generatePasswordResetTemplate(req.ResetURL),
	}

	return s.SendEmail(emailReq)
}

// generateWelcomeTemplate генерирует HTML шаблон для приветственного email
func (s *NotificationService) generateWelcomeTemplate(userName string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<meta http-equiv="X-UA-Compatible" content="IE=edge">
	<title>Добро пожаловать в MarkerLoop!</title>
</head>
<body style="margin: 0; padding: 0; background-color: #f4f4f4; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;">
	<table role="presentation" style="width: 100%%; border-collapse: collapse; background-color: #f4f4f4;">
		<tr>
			<td style="padding: 40px 20px;">
				<table role="presentation" style="max-width: 600px; margin: 0 auto; background-color: #ffffff; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1);">
					<tr>
						<td style="padding: 40px 40px 30px; text-align: center; background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%); border-radius: 8px 8px 0 0;">
							<h1 style="margin: 0; color: #ffffff; font-size: 32px; font-weight: 600; letter-spacing: -0.5px;">🎉 Добро пожаловать!</h1>
						</td>
					</tr>
					<tr>
						<td style="padding: 40px 40px 20px;">
							<p style="margin: 0 0 20px; color: #333333; font-size: 18px; line-height: 1.6; font-weight: 500;">Здравствуйте, %s!</p>
							<p style="margin: 0 0 30px; color: #666666; font-size: 16px; line-height: 1.6;">Спасибо за регистрацию в MarkerLoop! Мы рады приветствовать вас в нашем сообществе и готовы помочь вам достичь ваших целей.</p>
							<div style="background: linear-gradient(135deg, #f5f7fa 0%%, #c3cfe2 100%%); padding: 30px; border-radius: 8px; margin: 30px 0;">
								<h3 style="margin: 0 0 20px; color: #667eea; font-size: 20px; font-weight: 600;">🚀 Что дальше?</h3>
								<ul style="margin: 0; padding-left: 20px; color: #495057; font-size: 15px; line-height: 1.8;">
									<li style="margin-bottom: 10px;">Изучите возможности платформы и найдите то, что вам нужно</li>
									<li style="margin-bottom: 10px;">Настройте свой профиль для лучшего опыта</li>
									<li style="margin-bottom: 0;">Начните создавать свои проекты и делиться результатами</li>
								</ul>
							</div>
							<div style="text-align: center; margin: 40px 0;">
								<a href="#" style="display: inline-block; background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%); color: #ffffff; text-decoration: none; padding: 14px 32px; border-radius: 6px; font-weight: 600; font-size: 16px; box-shadow: 0 4px 6px rgba(102, 126, 234, 0.3);">Начать работу</a>
							</div>
							<p style="margin: 30px 0 0; color: #999999; font-size: 14px; line-height: 1.5;">Если у вас есть вопросы, не стесняйтесь обращаться в нашу службу поддержки. Мы всегда готовы помочь!</p>
						</td>
					</tr>
					<tr>
						<td style="padding: 30px 40px; background-color: #f8f9fa; border-radius: 0 0 8px 8px; border-top: 1px solid #e9ecef;">
							<p style="margin: 0; color: #6c757d; font-size: 13px; line-height: 1.5; text-align: center;">
								С уважением,<br>
								<strong style="color: #495057;">%s</strong>
							</p>
						</td>
					</tr>
				</table>
			</td>
		</tr>
	</table>
</body>
</html>`, userName, s.emailTemplates.CompanyName)
}

// generatePasswordResetTemplate генерирует HTML шаблон для email сброса пароля
func (s *NotificationService) generatePasswordResetTemplate(resetURL string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<meta http-equiv="X-UA-Compatible" content="IE=edge">
	<title>Сброс пароля</title>
</head>
<body style="margin: 0; padding: 0; background-color: #f4f4f4; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;">
	<table role="presentation" style="width: 100%%; border-collapse: collapse; background-color: #f4f4f4;">
		<tr>
			<td style="padding: 40px 20px;">
				<table role="presentation" style="max-width: 600px; margin: 0 auto; background-color: #ffffff; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1);">
					<tr>
						<td style="padding: 40px 40px 30px; text-align: center; background: linear-gradient(135deg, #f093fb 0%%, #f5576c 100%%); border-radius: 8px 8px 0 0;">
							<h1 style="margin: 0; color: #ffffff; font-size: 28px; font-weight: 600; letter-spacing: -0.5px;">🔐 Сброс пароля</h1>
						</td>
					</tr>
					<tr>
						<td style="padding: 40px 40px 20px;">
							<p style="margin: 0 0 20px; color: #333333; font-size: 16px; line-height: 1.6;">Здравствуйте!</p>
							<p style="margin: 0 0 30px; color: #666666; font-size: 16px; line-height: 1.6;">Вы запросили сброс пароля для вашего аккаунта в MarkerLoop. Нажмите на кнопку ниже, чтобы создать новый пароль:</p>
							<div style="text-align: center; margin: 40px 0;">
								<a href="%s" style="display: inline-block; background: linear-gradient(135deg, #f093fb 0%%, #f5576c 100%%); color: #ffffff; text-decoration: none; padding: 16px 40px; border-radius: 6px; font-weight: 600; font-size: 16px; box-shadow: 0 4px 6px rgba(245, 87, 108, 0.3);">Сбросить пароль</a>
							</div>
							<div style="background-color: #fff3cd; border-left: 4px solid #ffc107; padding: 15px 20px; margin: 30px 0; border-radius: 4px;">
								<p style="margin: 0; color: #856404; font-size: 14px; line-height: 1.5;">
									<strong>⏱ Важно:</strong> Эта ссылка действительна в течение <strong>%s</strong>. После истечения срока действия вам потребуется запросить новую ссылку.
								</p>
							</div>
							<p style="margin: 30px 0 20px; color: #999999; font-size: 14px; line-height: 1.5;">Если кнопка не работает, скопируйте и вставьте следующую ссылку в браузер:</p>
							<div style="background-color: #f8f9fa; padding: 15px; border-radius: 6px; border: 1px solid #e9ecef; margin-bottom: 30px;">
								<p style="margin: 0; word-break: break-all; color: #495057; font-size: 13px; line-height: 1.6; font-family: 'Courier New', monospace;">%s</p>
							</div>
							<div style="background-color: #f8d7da; border-left: 4px solid #dc3545; padding: 15px 20px; margin: 30px 0; border-radius: 4px;">
								<p style="margin: 0; color: #721c24; font-size: 14px; line-height: 1.5;">
									<strong>⚠️ Безопасность:</strong> Если вы не запрашивали сброс пароля, просто проигнорируйте это письмо. Ваш пароль останется без изменений.
								</p>
							</div>
						</td>
					</tr>
					<tr>
						<td style="padding: 30px 40px; background-color: #f8f9fa; border-radius: 0 0 8px 8px; border-top: 1px solid #e9ecef;">
							<p style="margin: 0; color: #6c757d; font-size: 13px; line-height: 1.5; text-align: center;">
								С уважением,<br>
								<strong style="color: #495057;">%s</strong>
							</p>
						</td>
					</tr>
				</table>
			</td>
		</tr>
	</table>
</body>
</html>`, resetURL, s.emailTemplates.PasswordResetValidity, resetURL, s.emailTemplates.CompanyName)
}
