package email

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// GoogleSMTPAdapter адаптер для отправки email через Google SMTP Relay (production)
// Поддерживает STARTTLS (порт 587) и SMTPS (порт 465)
type GoogleSMTPAdapter struct {
	host         string
	port         int
	user         string
	password     string
	from         string
	ehloHostname string
	timeout      time.Duration
	tlsConfig    *tls.Config
	logger       *logrus.Logger
}

// NewGoogleSMTPAdapter создает новый адаптер для Google SMTP Relay
func NewGoogleSMTPAdapter(host string, port int, user, password, from string, timeout time.Duration, logger *logrus.Logger) *GoogleSMTPAdapter {
	// EHLO hostname — домен отправителя. Нельзя использовать host сервера
	// (smtp-relay.gmail.com) — Google расценивает это как impersonation и режет 421.
	ehloHostname := "localhost"
	if at := strings.LastIndex(from, "@"); at >= 0 && at < len(from)-1 {
		ehloHostname = from[at+1:]
	}

	return &GoogleSMTPAdapter{
		host:         host,
		port:         port,
		user:         user,
		password:     password,
		from:         from,
		ehloHostname: ehloHostname,
		timeout:      timeout,
		tlsConfig: &tls.Config{
			ServerName:         host,
			InsecureSkipVerify: false,
			MinVersion:         tls.VersionTLS12,
		},
		logger: logger,
	}
}

// SendEmail отправляет email через Google SMTP Relay (с TLS)
func (a *GoogleSMTPAdapter) SendEmail(ctx context.Context, to, subject, body string) error {
	startTime := time.Now()
	a.logger.Infof("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	a.logger.Infof("📧 [Google SMTP] Начало отправки email")
	a.logger.Infof("   Тема: %s", subject)
	a.logger.Infof("   Отправитель: %s", a.from)
	a.logger.Infof("   SMTP сервер: %s:%d", a.host, a.port)
	a.logger.Infof("   Режим: с TLS (production)")
	a.logger.Infof("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// Создаем dialer с поддержкой контекста
	dialer := &net.Dialer{
		Timeout: a.timeout,
	}

	addr := fmt.Sprintf("%s:%d", a.host, a.port)
	a.logger.Debugf("🔌 Установка TCP соединения к %s...", addr)
	connectStartTime := time.Now()

	var conn net.Conn
	var err error

	// Для порта 465 используем TLS с самого начала (implicit TLS)
	if a.port == 465 {
		a.logger.Debug("Использование порта 465 (SMTPS, implicit TLS)")
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, a.tlsConfig)
		if err != nil {
			connectDuration := time.Since(connectStartTime)
			a.logger.Errorf("❌ Ошибка подключения к SMTP серверу %s (время попытки: %v): %v",
				addr, connectDuration.Round(time.Millisecond), err)
			return fmt.Errorf("ошибка подключения к SMTP: %w", err)
		}
		defer conn.Close()

		connectDuration := time.Since(connectStartTime)
		a.logger.Infof("✅ TCP соединение с TLS установлено за %v", connectDuration.Round(time.Millisecond))

		// Для порта 465 используем ручной SMTP протокол
		return a.sendEmailSMTPS(ctx, conn, to, subject, body, startTime)
	}

	// Для порта 587 используем обычное соединение + STARTTLS
	a.logger.Debugf("Использование порта %d (STARTTLS)", a.port)
	conn, err = dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		connectDuration := time.Since(connectStartTime)
		a.logger.Errorf("❌ Ошибка подключения к SMTP серверу %s (время попытки: %v): %v",
			addr, connectDuration.Round(time.Millisecond), err)
		return fmt.Errorf("ошибка подключения к SMTP: %w", err)
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
	client, err := smtp.NewClient(conn, a.ehloHostname)
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

	// TLS handshake (STARTTLS)
	a.logger.Debug("🔒 Выполнение STARTTLS...")
	if err := client.StartTLS(a.tlsConfig); err != nil {
		a.logger.Errorf("❌ Ошибка STARTTLS: %v", err)
		return fmt.Errorf("ошибка STARTTLS: %w", err)
	}
	a.logger.Debug("✅ STARTTLS выполнен успешно")

	// Проверяем контекст после TLS
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Аутентификация (если требуется)
	if a.user != "" && a.password != "" {
		a.logger.Debug("🔐 Выполнение аутентификации...")
		auth := smtp.PlainAuth("", a.user, a.password, a.host)
		if err := client.Auth(auth); err != nil {
			a.logger.Errorf("❌ Ошибка аутентификации: %v", err)
			return fmt.Errorf("ошибка аутентификации: %w", err)
		}
		a.logger.Debug("✅ Аутентификация выполнена успешно")
	} else {
		a.logger.Debug("⏭️  Аутентификация не требуется (SMTP Relay)")
	}

	// Отправка письма
	return a.sendEmailWithClient(ctx, client, to, subject, body, startTime)
}

// sendEmailSMTPS отправляет email через порт 465 (SMTPS) используя ручной SMTP протокол
func (a *GoogleSMTPAdapter) sendEmailSMTPS(ctx context.Context, conn net.Conn, to, subject, body string, startTime time.Time) error {
	a.logger.Debug("🔌 Использование ручного SMTP протокола для порта 465...")

	// Читаем приветствие сервера (220)
	a.logger.Infof("📥 Ожидание приветствия сервера (220)...")
	greeting, err := a.readSMTPResponse(conn)
	if err != nil {
		a.logger.Errorf("❌ Ошибка чтения приветствия сервера: %v", err)
		return fmt.Errorf("ошибка чтения приветствия сервера: %w", err)
	}
	a.logger.Infof("📥 Приветствие сервера получено: %q", greeting)

	greetingTrimmed := strings.TrimSpace(greeting)
	greetingCode := ""
	if len(greetingTrimmed) >= 3 {
		greetingCode = greetingTrimmed[:3]
	}
	if greetingCode != "220" {
		a.logger.Errorf("❌ Неожиданное приветствие сервера (код %s): %s", greetingCode, greetingTrimmed)
		return fmt.Errorf("неожиданное приветствие сервера (код %s): %s", greetingCode, greetingTrimmed)
	}
	a.logger.Infof("✅ Приветствие сервера получено (код %s)", greetingCode)

	// EHLO команда
	a.logger.Debug("📤 Отправка EHLO...")
	ehloCmd := fmt.Sprintf("EHLO %s\r\n", a.ehloHostname)
	if _, err := fmt.Fprint(conn, ehloCmd); err != nil {
		return fmt.Errorf("ошибка отправки EHLO: %w", err)
	}

	// Читаем ответ на EHLO
	a.logger.Infof("📥 Ожидание ответа на EHLO...")
	response, err := a.readSMTPResponse(conn)
	if err != nil {
		a.logger.Errorf("❌ Ошибка чтения ответа EHLO: %v", err)
		return fmt.Errorf("ошибка чтения ответа EHLO: %w", err)
	}
	a.logger.Infof("📥 EHLO ответ получен: %q", response)

	responseTrimmed := strings.TrimSpace(response)
	responseCode := ""
	if len(responseTrimmed) >= 3 {
		responseCode = responseTrimmed[:3]
	}
	if responseCode == "421" {
		a.logger.Errorf("❌ Сервер закрывает соединение после EHLO (421): %s", responseTrimmed)
		return fmt.Errorf("сервер SMTP недоступен (421) после EHLO: %s", responseTrimmed)
	}
	if responseCode != "250" {
		a.logger.Errorf("❌ Неожиданный ответ на EHLO (код %s): %s", responseCode, responseTrimmed)
		return fmt.Errorf("неожиданный ответ на EHLO (код %s): %s", responseCode, responseTrimmed)
	}
	a.logger.Infof("✅ EHLO успешно (код %s)", responseCode)

	// Аутентификация (если требуется)
	if a.user != "" && a.password != "" {
		a.logger.Debug("🔐 Выполнение аутентификации...")
		if err := a.authenticateSMTPS(ctx, conn); err != nil {
			return fmt.Errorf("ошибка аутентификации: %w", err)
		}
		a.logger.Debug("✅ Аутентификация выполнена успешно")
	} else {
		a.logger.Debug("⏭️  Аутентификация не требуется (SMTP Relay)")
	}

	// Отправка письма через ручной протокол
	return a.sendEmailManually(ctx, conn, to, subject, body, startTime)
}

// authenticateSMTPS выполняет аутентификацию для порта 465
func (a *GoogleSMTPAdapter) authenticateSMTPS(ctx context.Context, conn net.Conn) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	authData := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("\x00%s\x00%s", a.user, a.password)))
	if _, err := fmt.Fprintf(conn, "AUTH PLAIN %s\r\n", authData); err != nil {
		return fmt.Errorf("ошибка отправки AUTH PLAIN: %w", err)
	}

	response, err := a.readSMTPResponse(conn)
	if err != nil {
		return fmt.Errorf("ошибка чтения ответа аутентификации: %w", err)
	}
	a.logger.Debugf("Аутентификация ответ: %s", response)

	if !strings.HasPrefix(response, "235") {
		return fmt.Errorf("аутентификация не удалась: %s", response)
	}

	return nil
}

// sendEmailManually отправляет email используя ручной SMTP протокол
func (a *GoogleSMTPAdapter) sendEmailManually(ctx context.Context, conn net.Conn, to, subject, body string, startTime time.Time) error {
	a.logger.Debug("📤 Отправка письма через ручной протокол...")
	sendStartTime := time.Now()

	// MAIL FROM
	mailFromCmd := fmt.Sprintf("MAIL FROM:<%s>\r\n", a.from)
	if _, err := fmt.Fprint(conn, mailFromCmd); err != nil {
		return fmt.Errorf("ошибка отправки MAIL FROM: %w", err)
	}
	response, err := a.readSMTPResponse(conn)
	if err != nil {
		return fmt.Errorf("ошибка чтения ответа MAIL FROM: %w", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(response), "250") {
		return fmt.Errorf("ошибка MAIL FROM: %s", response)
	}

	// RCPT TO
	rcptCmd := fmt.Sprintf("RCPT TO:<%s>\r\n", to)
	if _, err := fmt.Fprint(conn, rcptCmd); err != nil {
		return fmt.Errorf("ошибка отправки RCPT TO: %w", err)
	}
	response, err = a.readSMTPResponse(conn)
	if err != nil {
		return fmt.Errorf("ошибка чтения ответа RCPT TO: %w", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(response), "250") {
		return fmt.Errorf("ошибка RCPT TO: %s", response)
	}

	// DATA
	if _, err := fmt.Fprint(conn, "DATA\r\n"); err != nil {
		return fmt.Errorf("ошибка отправки DATA: %w", err)
	}
	response, err = a.readSMTPResponse(conn)
	if err != nil {
		return fmt.Errorf("ошибка чтения ответа DATA: %w", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(response), "354") {
		return fmt.Errorf("неожиданный ответ на DATA (ожидался 354): %s", response)
	}

	// Генерируем Message-ID
	messageID := generateMessageID(a.from)

	// Формируем сообщение с заголовками
	message := buildMessage(a.from, to, subject, body, messageID)

	// Отправляем письмо с точкой в конце
	emailData := message + "\r\n.\r\n"
	if _, err := fmt.Fprint(conn, emailData); err != nil {
		return fmt.Errorf("ошибка отправки письма: %w", err)
	}

	// Читаем финальный ответ
	response, err = a.readSMTPResponse(conn)
	if err != nil {
		return fmt.Errorf("ошибка чтения финального ответа: %w", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(response), "250") {
		return fmt.Errorf("ошибка отправки письма (ожидался 250 OK): %s", response)
	}

	sendDuration := time.Since(sendStartTime)
	totalDuration := time.Since(startTime)

	a.logger.Infof("📨 Письмо принято SMTP-relay (код 250) за %v", sendDuration.Round(time.Millisecond))
	a.logger.Infof("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	a.logger.Infof("📨 Письмо принято relay в очередь (250 = приём, не доставка)")
	a.logger.Infof("   Message-ID: %s", messageID)
	a.logger.Infof("   Общее время: %v", totalDuration.Round(time.Millisecond))
	a.logger.Infof("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	return nil
}

// sendEmailWithClient отправляет email используя стандартный smtp.Client
func (a *GoogleSMTPAdapter) sendEmailWithClient(ctx context.Context, client *smtp.Client, to, subject, body string, startTime time.Time) (retErr error) {
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

	a.logger.Infof("📨 Письмо принято SMTP-relay (код 250) за %v", sendDuration.Round(time.Millisecond))
	a.logger.Infof("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	a.logger.Infof("📨 Письмо принято relay в очередь (250 = приём, не доставка)")
	a.logger.Infof("   Message-ID: %s", messageID)
	a.logger.Infof("   Общее время: %v", totalDuration.Round(time.Millisecond))
	a.logger.Infof("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	return nil
}

// readSMTPResponse читает ответ SMTP сервера (может быть многострочным)
func (a *GoogleSMTPAdapter) readSMTPResponse(conn net.Conn) (string, error) {
	var response strings.Builder
	buf := make([]byte, 512)
	readCount := 0

	for {
		readCount++
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, err := conn.Read(buf)
		conn.SetReadDeadline(time.Time{})

		if err != nil {
			if response.Len() > 0 {
				a.logger.Debugf("Частичный ответ перед ошибкой (чтение #%d): %q, ошибка: %v", readCount, response.String(), err)
			}
			return response.String(), err
		}
		if n == 0 {
			continue
		}

		response.Write(buf[:n])
		respStr := response.String()

		if strings.Contains(respStr, "\r\n") {
			lines := strings.Split(respStr, "\r\n")
			nonEmptyLines := []string{}
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					nonEmptyLines = append(nonEmptyLines, line)
				}
			}

			if len(nonEmptyLines) > 0 {
				lastLine := nonEmptyLines[len(nonEmptyLines)-1]
				if len(lastLine) >= 4 && lastLine[3] == ' ' {
					return respStr, nil
				}
			}
		}

		if response.Len() > 4096 {
			return response.String(), fmt.Errorf("ответ SMTP слишком длинный (%d байт)", response.Len())
		}
	}
}
