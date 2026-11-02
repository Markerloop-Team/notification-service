package email

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"time"

	"github.com/sirupsen/logrus"
)

// MailHogAdapter адаптер для отправки email через MailHog (локальная разработка)
// MailHog не поддерживает TLS, поэтому используется простое SMTP соединение
type MailHogAdapter struct {
	host    string
	port    int
	from    string
	timeout time.Duration
	logger  *logrus.Logger
}

// NewMailHogAdapter создает новый адаптер для MailHog
func NewMailHogAdapter(host string, port int, from string, timeout time.Duration, logger *logrus.Logger) *MailHogAdapter {
	return &MailHogAdapter{
		host:    host,
		port:    port,
		from:    from,
		timeout: timeout,
		logger:  logger,
	}
}

// SendEmail отправляет email через MailHog (без TLS)
func (a *MailHogAdapter) SendEmail(ctx context.Context, to, subject, body string) error {
	startTime := time.Now()
	a.logger.Infof("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	a.logger.Infof("📧 [MailHog] Начало отправки email")
	a.logger.Infof("   Тема: %s", subject)
	a.logger.Infof("   Отправитель: %s", a.from)
	a.logger.Infof("   SMTP сервер: %s:%d", a.host, a.port)
	a.logger.Infof("   Режим: без TLS (локальная разработка)")
	a.logger.Infof("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// Создаем dialer с поддержкой контекста
	dialer := &net.Dialer{
		Timeout: a.timeout,
	}

	addr := fmt.Sprintf("%s:%d", a.host, a.port)
	a.logger.Debugf("🔌 Установка TCP соединения к %s...", addr)
	connectStartTime := time.Now()

	// MailHog использует простое TCP соединение без TLS
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		connectDuration := time.Since(connectStartTime)
		a.logger.Errorf("❌ Ошибка подключения к MailHog %s (время попытки: %v): %v",
			addr, connectDuration.Round(time.Millisecond), err)
		return fmt.Errorf("ошибка подключения к MailHog: %w", err)
	}
	defer conn.Close()

	connectDuration := time.Since(connectStartTime)
	a.logger.Infof("✅ TCP соединение установлено за %v", connectDuration.Round(time.Millisecond))

	// Проверяем контекст после подключения
	select {
	case <-ctx.Done():
		a.logger.Warnf("Контекст отменен после подключения: %v", ctx.Err())
		return ctx.Err()
	default:
	}

	// Создаем SMTP клиент
	a.logger.Debug("🔌 Создание SMTP клиента...")
	client, err := smtp.NewClient(conn, a.host)
	if err != nil {
		a.logger.Errorf("❌ Ошибка создания SMTP клиента: %v", err)
		return fmt.Errorf("ошибка создания SMTP клиента: %w", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			a.logger.Warnf("Ошибка закрытия SMTP клиента: %v", err)
		}
	}()

	// Проверяем контекст после создания клиента
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// MailHog не требует аутентификации и TLS
	a.logger.Debug("⏭️  Пропуск STARTTLS и аутентификации (MailHog)")

	// Отправка письма
	return a.sendEmailWithClient(ctx, client, to, subject, body, startTime)
}

// sendEmailWithClient отправляет email используя стандартный smtp.Client
func (a *MailHogAdapter) sendEmailWithClient(ctx context.Context, client *smtp.Client, to, subject, body string, startTime time.Time) (retErr error) {
	// SDL-738: ответы SMTP на любом шаге могут эхом нести адрес получателя.
	defer func() { retErr = RedactRecipient(retErr, to) }()
	a.logger.Debug("📤 Отправка письма...")
	sendStartTime := time.Now()

	// MAIL FROM
	if err := client.Mail(a.from); err != nil {
		a.logger.Errorf("❌ Ошибка MAIL FROM: %v", RedactRecipient(err, to))
		return fmt.Errorf("ошибка MAIL FROM: %w", err)
	}

	// RCPT TO
	if err := client.Rcpt(to); err != nil {
		a.logger.Errorf("❌ Ошибка RCPT TO: %v", RedactRecipient(err, to))
		return fmt.Errorf("ошибка RCPT TO: %w", err)
	}

	// DATA
	writer, err := client.Data()
	if err != nil {
		a.logger.Errorf("❌ Ошибка DATA: %v", RedactRecipient(err, to))
		return fmt.Errorf("ошибка DATA: %w", err)
	}

	// Генерируем Message-ID
	messageID := generateMessageID(a.from)

	// Формируем сообщение с заголовками
	message := buildMessage(a.from, to, subject, body, messageID)

	// Записываем письмо
	if _, err := writer.Write([]byte(message)); err != nil {
		a.logger.Errorf("❌ Ошибка записи письма: %v", RedactRecipient(err, to))
		return fmt.Errorf("ошибка записи письма: %w", err)
	}

	// Закрываем writer (отправляет письмо)
	if err := writer.Close(); err != nil {
		a.logger.Errorf("❌ Ошибка закрытия writer: %v", RedactRecipient(err, to))
		return fmt.Errorf("ошибка закрытия writer: %w", err)
	}

	sendDuration := time.Since(sendStartTime)
	totalDuration := time.Since(startTime)

	a.logger.Infof("📨 Письмо принято MailHog за %v", sendDuration.Round(time.Millisecond))
	a.logger.Infof("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	a.logger.Infof("📨 Письмо принято MailHog (локальная разработка)")
	a.logger.Infof("   Message-ID: %s", messageID)
	a.logger.Infof("   Общее время: %v", totalDuration.Round(time.Millisecond))
	a.logger.Infof("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	return nil
}
