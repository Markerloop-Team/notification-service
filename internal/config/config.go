package config

import (
	"os"
	"strconv"
	"time"

	"gitlab.com/markerloop/go-sdk/pkg/config"
)

// Config конфигурация notification-service
type Config struct {
	*config.BaseConfig
	Email          EmailConfig
	EmailTemplates EmailTemplatesConfig
	Telegram       TelegramConfig
	// ServiceKey — общий ключ сервис-сервисного контура. Тот же самый
	// SERVICE_KEY, что уходит в Telegram.ServiceKey исходящим вызовам к боту;
	// здесь он нужен ВХОДЯЩЕЙ стороне — группе /internal (SDL-322). Отдельной
	// переменной окружения не заводим: ключ у контура один, и второй источник
	// правды означал бы, что половина запросов ходит с одним значением, а
	// проверяется по другому.
	ServiceKey string
}

// TelegramConfig конфигурация Telegram канала
type TelegramConfig struct {
	BotServiceURL string
	ServiceKey    string
	Timeout       time.Duration
	// EP-56 / SDL-346: ретраи доставки к боту. RetryAttempts — общее число
	// попыток (не «дополнительных»), RetryBackoff — база экспоненциальной
	// паузы между ними.
	//
	// SDL-351: значение подобрано под окно rolling-update бота, а не «на глаз».
	// При базе 500ms паузы идут 0.5s, 1s, 2s, 4s, 8s, поэтому шестая попытка
	// стартует примерно на 15.5s — внутри totalDeliveryBudget (25s), который и
	// остаётся настоящим потолком. Прежние 3 попытки выдыхались за ~1.5s, то
	// есть тратили 6% отпущенного бюджета и не переживали даже штатной замены
	// пода: 21 Aug 2026 deploy-уведомление о выкате бота было потеряно ровно
	// так (Sentry MARKERLOOP-GO-37, connection refused ×3).
	RetryAttempts int
	RetryBackoff  time.Duration
}

// EmailConfig конфигурация email
type EmailConfig struct {
	Host       string
	Port       int
	User       string
	Password   string
	From       string
	Timeout    time.Duration
	DisableTLS bool // Отключить TLS (для локальной разработки с mailhog)
}

// EmailTemplatesConfig конфигурация шаблонов email
type EmailTemplatesConfig struct {
	VerificationSubject      string
	VerificationCodeValidity string
	WelcomeSubject           string
	PasswordResetSubject     string
	PasswordResetValidity    string
	CompanyName              string
}

// Load загружает конфигурацию из переменных окружения
func Load() *Config {
	return &Config{
		BaseConfig: &config.BaseConfig{
			Server: config.ServerConfig{
				Port:         getEnv("PORT", "8083"),
				Host:         getEnv("HOST", "0.0.0.0"),
				ReadTimeout:  getEnvDuration("READ_TIMEOUT", 30*time.Second),
				WriteTimeout: getEnvDuration("WRITE_TIMEOUT", 30*time.Second),
				IdleTimeout:  getEnvDuration("IDLE_TIMEOUT", 60*time.Second),
			},
			Log: config.LogConfig{
				Level: getEnv("LOG_LEVEL", "info"),
			},
		},
		Email:      *LoadEmailConfig(),
		ServiceKey: getEnv("SERVICE_KEY", ""),
		Telegram: TelegramConfig{
			BotServiceURL: getEnv("TELEGRAM_BOT_SERVICE_URL", "http://markerloop-bot:8085"),
			ServiceKey:    getEnv("SERVICE_KEY", ""),
			Timeout:       getEnvDuration("TELEGRAM_HTTP_TIMEOUT", 10*time.Second),
			RetryAttempts: getEnvInt("TELEGRAM_RETRY_ATTEMPTS", 6),
			RetryBackoff:  getEnvDuration("TELEGRAM_RETRY_BACKOFF", 500*time.Millisecond),
		},
		EmailTemplates: EmailTemplatesConfig{
			VerificationSubject:      getEnv("EMAIL_VERIFICATION_SUBJECT", "Подтверждение email адреса"),
			VerificationCodeValidity: getEnv("EMAIL_VERIFICATION_VALIDITY", "15 минут"),
			WelcomeSubject:           getEnv("EMAIL_WELCOME_SUBJECT", "Добро пожаловать в MarkerLoop!"),
			PasswordResetSubject:     getEnv("EMAIL_PASSWORD_RESET_SUBJECT", "Сброс пароля"),
			PasswordResetValidity:    getEnv("EMAIL_PASSWORD_RESET_VALIDITY", "1 час"),
			CompanyName:              getEnv("EMAIL_COMPANY_NAME", "Команда MarkerLoop"),
		},
	}
}

// getEnv получает значение переменной окружения или возвращает значение по умолчанию
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvDuration получает значение переменной окружения как duration
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

// LoadEmailConfig загружает конфигурацию email
func LoadEmailConfig() *EmailConfig {
	host := getEnv("MAIL_HOST", "localhost")
	port := getEnvInt("MAIL_PORT", 1025)

	// Автоматически отключаем TLS для MailHog (localhost:1025)
	// если явно не указано иное через MAIL_DISABLE_TLS
	disableTLS := getEnvBool("MAIL_DISABLE_TLS", false)
	if !disableTLS && (host == "localhost" || host == "127.0.0.1") && port == 1025 {
		disableTLS = true
	}

	return &EmailConfig{
		Host:       host,
		Port:       port,
		User:       getEnv("MAIL_USER", ""),
		Password:   getEnv("MAIL_PASSWORD", ""),
		From:       getEnv("FROM_EMAIL", "noreply@markerloop.com"),
		Timeout:    getEnvDuration("MAIL_TIMEOUT", 30*time.Second),
		DisableTLS: disableTLS,
	}
}

// getEnvInt получает целочисленное значение переменной окружения
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvBool получает булево значение переменной окружения
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}
