package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"

	"notification-service/internal/config"
	"notification-service/internal/handlers"
	"notification-service/internal/routes"
	"notification-service/internal/services"

	// Импорт go-sdk
	"gitlab.com/markerloop/go-sdk/pkg/logger"
	sentrysdk "gitlab.com/markerloop/go-sdk/pkg/sentry"
)

// @title Notification Service API
// @version 1.0
// @description Сервис отправки email-уведомлений Markerloop
// @BasePath /api/v1
// @securityDefinitions.apikey ServiceKey
// @in header
// @name X-Service-Key
// @description Ключ для service-to-service аутентификации
func main() {
	// Загружаем переменные окружения
	if err := godotenv.Load(); err != nil {
		logrus.Println("Файл .env не найден, используем переменные окружения по умолчанию")
	}

	// Загружаем конфигурацию
	cfg := config.Load()

	// Инициализируем логгер
	log := logger.InitLogger("notification-service")

	// Sentry (SDL-86): включается только когда контур задал SENTRY_DSN;
	// без переменной — рабочий no-op, сервис стартует как раньше.
	sentrysdk.Init("notification-service", log)
	defer sentrysdk.Flush()

	// Создаем сервисы
	notificationService := services.NewNotificationService(cfg.Email, cfg.Telegram, cfg.EmailTemplates, log)

	// Создаем хендлеры
	notificationHandler := handlers.NewNotificationHandler(*notificationService, log)

	// Настраиваем маршруты
	router := routes.SetupRoutes(notificationHandler, log, cfg.ServiceKey)

	// Создаем HTTP сервер с timeout'ами
	server := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Запускаем сервер в горутине
	go func() {
		log.Infof("Notification service запущен на порту %s", cfg.Server.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Ошибка запуска сервера:", err)
		}
	}()

	// Ожидаем сигнал для graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("Завершение работы сервера...")

	// Graceful shutdown с timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatal("Ошибка при завершении работы сервера:", err)
	}

	log.Info("Сервер успешно завершил работу")
}
