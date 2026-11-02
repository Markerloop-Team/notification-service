package email

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// SDL-738: ответ SMTP на любом шаге (не только RCPT TO) может эхом нести
// адрес получателя, причём в другом регистре. Ни журнал (уровень Error
// уходит в Sentry), ни возвращаемая ошибка его нести не должны.

const (
	probeTo    = "Leak-Probe@Example.org"
	probeEcho  = "LEAK-PROBE@EXAMPLE.ORG"
	probeLower = "leak-probe@example.org"
)

// fakeSMTP принимает одно соединение и отвечает 250 на всё, кроме DATA:
// там — 554 с адресом получателя в верхнем регистре.
func fakeSMTP(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				_, _ = c.Write([]byte("220 fake ESMTP\r\n"))
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					cmd := strings.ToUpper(strings.TrimSpace(line))
					switch {
					case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
						_, _ = c.Write([]byte("250 fake\r\n"))
					case cmd == "DATA":
						_, _ = c.Write([]byte("554 5.7.1 <" + probeEcho + ">: message rejected\r\n"))
					case cmd == "QUIT":
						_, _ = c.Write([]byte("221 bye\r\n"))
						return
					default:
						_, _ = c.Write([]byte("250 ok\r\n"))
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func captureLogger() (*logrus.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	l := logrus.New()
	l.SetOutput(buf)
	l.SetLevel(logrus.DebugLevel)
	return l, buf
}

func dialClient(t *testing.T, addr string) *smtp.Client {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	c, err := smtp.NewClient(conn, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func assertNoAddr(t *testing.T, what, s string) {
	t.Helper()
	if strings.Contains(strings.ToLower(s), probeLower) {
		t.Fatalf("%s содержит адрес получателя:\n%s", what, s)
	}
}

func TestDataErrorRedactedInLogAndError(t *testing.T) {
	addr := fakeSMTP(t)
	type sender func(*smtp.Client, *logrus.Logger) error
	cases := map[string]sender{
		"google": func(c *smtp.Client, l *logrus.Logger) error {
			a := NewGoogleSMTPAdapter("127.0.0.1", 587, "", "", "noreply@markerloop.com", time.Second, l)
			return a.sendEmailWithClient(context.Background(), c, probeTo, "s", "b", time.Now())
		},
		"mailhog": func(c *smtp.Client, l *logrus.Logger) error {
			a := NewMailHogAdapter("127.0.0.1", 1025, "noreply@markerloop.com", time.Second, l)
			return a.sendEmailWithClient(context.Background(), c, probeTo, "s", "b", time.Now())
		},
	}
	for name, send := range cases {
		logger, buf := captureLogger()
		err := send(dialClient(t, addr), logger)
		if err == nil {
			t.Fatalf("%s: ожидалась ошибка DATA", name)
		}
		assertNoAddr(t, name+": лог", buf.String())
		assertNoAddr(t, name+": возвращаемая ошибка", err.Error())
		if !strings.Contains(buf.String(), "<recipient>") {
			t.Fatalf("%s: в логе нет отметки <recipient> — сценарий не дошёл до ошибки с адресом:\n%s", name, buf.String())
		}
	}
}

func TestRedactRecipientCaseInsensitive(t *testing.T) {
	cause := errors.New("554 <" + probeEcho + "> and " + probeLower)
	got := RedactRecipient(cause, probeTo)
	assertNoAddr(t, "ошибка", got.Error())
	if !errors.Is(got, cause) {
		t.Fatal("Unwrap потерял исходную ошибку")
	}
	if RedactRecipient(nil, probeTo) != nil {
		t.Fatal("nil должен остаться nil")
	}
}
