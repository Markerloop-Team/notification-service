package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"notification-service/internal/handlers"

	// Импорт go-sdk
	"gitlab.com/markerloop/go-sdk/pkg/middleware"
	"gitlab.com/markerloop/go-sdk/pkg/requestid"
	sentrysdk "gitlab.com/markerloop/go-sdk/pkg/sentry"
	"gitlab.com/markerloop/go-sdk/pkg/wrapper"

	// Swagger docs
	_ "notification-service/docs"
)

// SetupRoutes настраивает маршруты для notification-service.
//
// serviceKey закрывает всю группу /api/v1/notifications: каждая ручка шлёт
// почту или сообщения, а зовут их только сервисы (auth, collections, шлюз,
// вебхук ArgoCD) — все с X-Service-Key. До этого ключ проверял только шлюз,
// и любой под в кластере мог слать письма по произвольному адресу.
// health и swagger остаются открытыми.
func SetupRoutes(notificationHandler *handlers.NotificationHandler, logger *logrus.Logger, serviceKey string) *gin.Engine {
	// Создаем роутер
	r := wrapper.Engine()

	// Добавляем middleware
	r.Use(requestid.Middleware()) // SDL-87: сквозной X-Request-Id
	// Sentry перед Recovery: паника долетает до sentry-middleware первым,
	// он же и отвечает 500; gin.Recovery остаётся страховкой ниже по цепи.
	r.Use(sentrysdk.GinMiddleware("notification-service"))
	r.Use(gin.Recovery())
	r.Use(middleware.Logger(logger))
	r.Use(middleware.CORS())

	// Health check
	r.GET("/health", middleware.HealthCheck("notification-service", "1.0.0"))

	// Swagger UI
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// API роуты
	api := wrapper.Group(r, "/api/v1/notifications")
	api.Use(middleware.ServiceToServiceAuth(serviceKey))
	{
		// Email уведомления
		email := api.Group("/email")
		{
			email.POST("/send", notificationHandler.SendEmail)
			email.POST("/send-verification", notificationHandler.SendVerificationEmail)
			email.POST("/send-welcome", notificationHandler.SendWelcomeEmail)
			email.POST("/send-password-reset", notificationHandler.SendPasswordResetEmail)
		}

		// Сервис-сервисная поверхность (SDL-322). Сегмент пути `internal` —
		// то же соглашение, что у auth-service (`/auth/internal`) и
		// user-service (`/api/v1/internal`): по нему джоба gw-contract
		// понимает, что роут не обязан публиковаться шлюзом, правилом, а не
		// списком исключений.
		//
		// TestEmail жил в публичной группе `/email` и попадал в спеку как
		// публичная операция, хотя шлюз его никогда не публиковал: снаружи
		// это читалось как «объявленный роут недостижим» (расхождение,
		// найденное SDL-319). Ручка рассылает почту по ПРОИЗВОЛЬНОМУ адресу,
		// поэтому публичной поверхностью ей быть нельзя, а тест-письма
		// отправляются и изнутри контура.
		internalGroup := api.Group("/internal")
		{
			internalGroup.POST("/email/test", notificationHandler.TestEmail)
			// SDL-615: приглашение в рабочее пространство. Зовёт
			// collections-service; адрес получателя произвольный, поэтому
			// только под ключом и только здесь — публичной группе `/email`
			// эта ручка не принадлежит.
			internalGroup.POST("/email/workspace-invite", notificationHandler.SendWorkspaceInviteEmail)
		}

		// Telegram уведомления
		tg := api.Group("/telegram")
		{
			tg.POST("/send", notificationHandler.SendTelegramMessage)
		}

		// Deploy уведомления (ArgoCD webhook)
		api.POST("/deploy", notificationHandler.DeployNotification)
	}

	return r
}
