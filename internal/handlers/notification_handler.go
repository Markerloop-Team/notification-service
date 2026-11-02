package handlers

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"notification-service/internal/models"
	"notification-service/internal/services"

	// Импорт go-sdk
	"gitlab.com/markerloop/go-sdk/pkg/response"
)

// NotificationHandler обрабатывает HTTP запросы для уведомлений
type NotificationHandler struct {
	notificationService services.NotificationService
	logger              *logrus.Logger
}

// NewNotificationHandler создает новый хендлер уведомлений
func NewNotificationHandler(notificationService services.NotificationService, logger *logrus.Logger) *NotificationHandler {
	return &NotificationHandler{
		notificationService: notificationService,
		logger:              logger,
	}
}

// SendEmail отправляет обычный email
// @Summary Отправить email
// @Description Отправляет произвольный email. Service-to-service endpoint.
// @Tags email
// @Accept json
// @Produce json
// @Security ServiceKey
// @Param request body models.EmailRequest true "Данные email"
// @Success 200 {object} models.EmailSuccessResponse
// @Failure 400 {object} models.ErrorResponseSwagger
// @Failure 500 {object} models.ErrorResponseSwagger
// @Router /notifications/email/send [post]
func (h *NotificationHandler) SendEmail(c *gin.Context) {
	var req models.EmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, "Ошибка валидации", err.Error())
		return
	}

	result, err := h.notificationService.SendEmail(&req)
	if err != nil {
		h.logger.WithContext(c.Request.Context()).Errorf("SendEmail: ошибка отправки, type=generic: %v", err)
		response.InternalServerError(c, "Ошибка отправки email")
		return
	}

	response.Success(c, result, result.Message)
}

// SendVerificationEmail отправляет email верификации
// @Summary Отправить email верификации
// @Description Отправляет email с кодом верификации. Service-to-service endpoint.
// @Tags email
// @Accept json
// @Produce json
// @Security ServiceKey
// @Param request body models.VerificationEmailRequest true "Email и код"
// @Success 200 {object} models.EmailSuccessResponse
// @Failure 400 {object} models.ErrorResponseSwagger
// @Failure 500 {object} models.ErrorResponseSwagger
// @Router /notifications/email/send-verification [post]
func (h *NotificationHandler) SendVerificationEmail(c *gin.Context) {
	var req models.VerificationEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warnf("SendVerificationEmail: ошибка валидации запроса: %v", err)
		response.ValidationError(c, "Ошибка валидации", err.Error())
		return
	}

	h.logger.WithContext(c.Request.Context()).Info("SendVerificationEmail: получен запрос, type=verification")
	result, err := h.notificationService.SendVerificationEmail(&req)
	if err != nil {
		h.logger.WithContext(c.Request.Context()).Errorf("SendVerificationEmail: ошибка отправки, type=verification: %v", err)
		response.InternalServerError(c, "Ошибка отправки email верификации")
		return
	}

	h.logger.WithContext(c.Request.Context()).Info("SendVerificationEmail: письмо принято к доставке, type=verification")
	response.Success(c, result, result.Message)
}

// SendWelcomeEmail отправляет приветственный email
// @Summary Отправить приветственный email
// @Description Отправляет welcome email новому пользователю. Service-to-service endpoint.
// @Tags email
// @Accept json
// @Produce json
// @Security ServiceKey
// @Param request body models.WelcomeEmailRequest true "Email и имя"
// @Success 200 {object} models.EmailSuccessResponse
// @Failure 400 {object} models.ErrorResponseSwagger
// @Failure 500 {object} models.ErrorResponseSwagger
// @Router /notifications/email/send-welcome [post]
func (h *NotificationHandler) SendWelcomeEmail(c *gin.Context) {
	var req models.WelcomeEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, "Ошибка валидации", err.Error())
		return
	}

	result, err := h.notificationService.SendWelcomeEmail(&req)
	if err != nil {
		h.logger.WithContext(c.Request.Context()).Errorf("SendWelcomeEmail: ошибка отправки, type=welcome: %v", err)
		response.InternalServerError(c, "Ошибка отправки приветственного email")
		return
	}

	response.Success(c, result, result.Message)
}

// SendPasswordResetEmail отправляет email сброса пароля
// @Summary Отправить email сброса пароля
// @Description Отправляет email с ссылкой для сброса пароля. Service-to-service endpoint.
// @Tags email
// @Accept json
// @Produce json
// @Security ServiceKey
// @Param request body models.PasswordResetEmailRequest true "Email и URL сброса"
// @Success 200 {object} models.EmailSuccessResponse
// @Failure 400 {object} models.ErrorResponseSwagger
// @Failure 500 {object} models.ErrorResponseSwagger
// @Router /notifications/email/send-password-reset [post]
func (h *NotificationHandler) SendPasswordResetEmail(c *gin.Context) {
	var req models.PasswordResetEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, "Ошибка валидации", err.Error())
		return
	}

	result, err := h.notificationService.SendPasswordResetEmail(&req)
	if err != nil {
		h.logger.WithContext(c.Request.Context()).Errorf("SendPasswordResetEmail: ошибка отправки, type=password_reset: %v", err)
		response.InternalServerError(c, "Ошибка отправки email сброса пароля")
		return
	}

	response.Success(c, result, result.Message)
}

// TestEmail отправляет тестовый email для проверки работы SMTP.
//
// Сервис-сервисная ручка: живёт под сегментом /internal и требует
// X-Service-Key (SDL-322). Публичной поверхностью быть не может — она
// отправляет письмо на ПРОИЗВОЛЬНЫЙ переданный адрес.
// @Summary Тестовый email (service-to-service)
// @Description Отправляет тестовое письмо для проверки SMTP. Требует X-Service-Key.
// @Tags email
// @Accept json
// @Produce json
// @Param X-Service-Key header string true "Ключ сервиса"
// @Param request body object true "Email получателя (to)"
// @Success 200 {object} models.EmailSuccessResponse
// @Failure 400 {object} models.ErrorResponseSwagger
// @Failure 401 {object} models.ErrorResponseSwagger
// @Failure 500 {object} models.ErrorResponseSwagger
// @Router /notifications/internal/email/test [post]
func (h *NotificationHandler) TestEmail(c *gin.Context) {
	var req struct {
		To string `json:"to" binding:"required,email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, "Ошибка валидации", err.Error())
		return
	}

	// Создаем тестовое письмо
	testBody := `
		<!DOCTYPE html>
		<html>
		<head>
			<meta charset="UTF-8">
			<title>Тестовое письмо</title>
		</head>
		<body>
			<div style="font-family: Arial, sans-serif; max-width: 600px; margin: 0 auto; padding: 20px;">
				<h2 style="color: #333;">Тестовое письмо от Markerloop</h2>
				<p>Это тестовое письмо для проверки работы SMTP.</p>
				<p>Если вы получили это письмо, значит SMTP настроен правильно!</p>
				<div style="background-color: #f8f9fa; padding: 15px; border-radius: 8px; margin: 20px 0;">
					<p style="margin: 0;"><strong>Время отправки:</strong> ` + c.GetHeader("Date") + `</p>
					<p style="margin: 5px 0 0 0;"><strong>Получатель:</strong> ` + req.To + `</p>
				</div>
				<hr style="border: none; border-top: 1px solid #eee; margin: 20px 0;">
				<p style="color: #666; font-size: 12px;">
					С уважением,<br>
					Команда MarkerLoop
				</p>
			</div>
		</body>
		</html>
	`

	testReq := &models.EmailRequest{
		To:      req.To,
		Subject: "Тестовое письмо от Markerloop",
		Body:    testBody,
	}

	result, err := h.notificationService.SendEmail(testReq)
	if err != nil {
		h.logger.Error("Ошибка отправки тестового email:", err)
		response.InternalServerError(c, "Ошибка отправки тестового email: "+err.Error())
		return
	}

	response.Success(c, result, "Тестовое письмо принято к доставке (SMTP 250)")
}

// SendWorkspaceInviteEmail отправляет письмо-приглашение в рабочее пространство.
//
// Сервис-сервисная ручка (SDL-615): живёт под /internal и требует
// X-Service-Key. Имя пространства и приглашающего — пользовательский ввод,
// шаблон экранирует их сам (html/template); тема письма фиксирована.
// @Summary Письмо-приглашение в рабочее пространство (service-to-service)
// @Description Отправляет приглашение в рабочее пространство на указанный адрес. Требует X-Service-Key. Локали: ru, en, kk (по умолчанию en).
// @Tags email
// @Accept json
// @Produce json
// @Param X-Service-Key header string true "Ключ сервиса"
// @Param request body models.WorkspaceInviteEmailRequest true "Данные приглашения"
// @Success 200 {object} models.EmailSuccessResponse
// @Failure 400 {object} models.ErrorResponseSwagger
// @Failure 401 {object} models.ErrorResponseSwagger
// @Failure 500 {object} models.ErrorResponseSwagger
// @Router /notifications/internal/email/workspace-invite [post]
func (h *NotificationHandler) SendWorkspaceInviteEmail(c *gin.Context) {
	var req models.WorkspaceInviteEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, "Ошибка валидации", err.Error())
		return
	}

	result, err := h.notificationService.SendWorkspaceInviteEmail(&req)
	if err != nil {
		var ve *services.InviteValidationError
		if errors.As(err, &ve) {
			// Тексты InviteValidationError — константы без входа: в accept_url
			// лежит токен приглашения, эхом в 400 он возвращаться не должен.
			response.ValidationError(c, "Ошибка валидации", ve.Error())
			return
		}
		h.logger.WithContext(c.Request.Context()).Errorf("SendWorkspaceInviteEmail: ошибка отправки, type=workspace_invite: %v", err)
		response.InternalServerError(c, "Ошибка отправки письма-приглашения")
		return
	}

	h.logger.WithContext(c.Request.Context()).Info("SendWorkspaceInviteEmail: письмо принято к доставке, type=workspace_invite")
	response.Success(c, result, result.Message)
}

// SendTelegramMessage отправляет Telegram сообщение пользователю
// @Summary Отправить Telegram сообщение
// @Description Отправляет Telegram уведомление пользователю через бот. Service-to-service endpoint.
// @Tags telegram
// @Accept json
// @Produce json
// @Security ServiceKey
// @Param request body models.TelegramSendRequest true "Данные сообщения"
// @Success 200 {object} models.TelegramSendResponse
// @Failure 400 {object} models.ErrorResponseSwagger
// @Failure 500 {object} models.ErrorResponseSwagger
// @Router /notifications/telegram/send [post]
func (h *NotificationHandler) SendTelegramMessage(c *gin.Context) {
	var req models.TelegramSendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Неверный формат данных")
		return
	}

	result, err := h.notificationService.SendTelegramMessage(&req)
	if err != nil {
		h.logger.Error("Ошибка отправки Telegram сообщения:", err)
		response.InternalServerError(c, "Ошибка отправки Telegram сообщения")
		return
	}

	response.Success(c, result, result.Message)
}

// DeployNotification принимает webhook от ArgoCD и пересылает в бот
// @Summary Deploy notification (ArgoCD webhook)
// @Description Принимает уведомление о деплое и пересылает подписанным пользователям через бот.
// @Tags deploy
// @Accept json
// @Produce json
// @Param request body models.DeployNotificationRequest true "Данные деплоя"
// @Success 200 {object} models.DeployNotificationResponse
// @Failure 400 {object} models.ErrorResponseSwagger
// @Failure 500 {object} models.ErrorResponseSwagger
// @Router /notifications/deploy [post]
func (h *NotificationHandler) DeployNotification(c *gin.Context) {
	var req models.DeployNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Неверный формат данных")
		return
	}

	result, err := h.notificationService.SendDeployNotification(&req)
	if err != nil {
		h.logger.Error("Ошибка отправки deploy notification:", err)
		response.InternalServerError(c, "Ошибка отправки deploy notification")
		return
	}

	response.Success(c, result, result.Message)
}
