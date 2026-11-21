package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"vetlog/internal/lead"
)

type Message struct {
	Subject string
	Body    string
}

type Sender interface {
	Send(ctx context.Context, m Message) error
}

type SMTPConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
	To       []string
}

// smtpTimeout ограничивает всю SMTP-сессию, чтобы зависший сервер не держал воркер.
const smtpTimeout = 30 * time.Second

// SMTP отправляет письма через submission-порт (587) с обязательным STARTTLS.
// Неявный TLS (порт 465) не поддерживается: конфигурация с 465 отклоняется при старте.
type SMTP struct {
	Config SMTPConfig
}

// Send открывает сессию: EHLO -> обязательный STARTTLS -> AUTH -> MAIL/RCPT/DATA.
// Если сервер не предлагает STARTTLS, письмо не отправляется (логин в открытом виде не уходит).
func (s SMTP) Send(_ context.Context, m Message) error {
	host := s.Config.Host
	addr := net.JoinHostPort(host, strconv.Itoa(s.Config.Port))

	conn, err := net.DialTimeout("tcp", addr, smtpTimeout)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	if err := conn.SetDeadline(time.Now().Add(smtpTimeout)); err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp deadline: %w", err)
	}

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer func() { _ = c.Close() }()

	if err := c.Hello("vetlog"); err != nil {
		return fmt.Errorf("smtp hello: %w", err)
	}
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return errors.New("smtp: server does not offer STARTTLS")
	}
	if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
		return fmt.Errorf("smtp starttls: %w", err)
	}
	if s.Config.User != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Config.User, s.Config.Password, host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(s.Config.From); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	for _, rcpt := range s.Config.To {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp rcpt %s: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(buildMessage(s.Config, m)); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp data close: %w", err)
	}
	return c.Quit()
}

func buildMessage(cfg SMTPConfig, m Message) []byte {
	raw := strings.Join([]string{
		"From: " + cfg.From,
		"To: " + strings.Join(cfg.To, ", "),
		"Subject: " + mime.QEncoding.Encode("utf-8", m.Subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		m.Body,
	}, "\r\n")
	return []byte(raw)
}

// LogOnly используется, когда SMTP не настроен: письмо не отправляется, факт логируется.
type LogOnly struct {
	Log *slog.Logger
}

func (l LogOnly) Send(_ context.Context, m Message) error {
	l.Log.Info("mail disabled, message not sent", slog.String("subject", m.Subject))
	return nil
}

// LeadMessage формирует письмо о новой заявке.
func LeadMessage(l lead.Lead) Message {
	email := l.Email
	if email == "" {
		email = "—"
	}
	return Message{
		Subject: "Новая заявка с сайта Ветлог",
		Body: fmt.Sprintf("Имя: %s\nТелефон: %s\nКлиника: %s\nEmail: %s\nЯзык страницы: %s\nВремя: %s\n",
			l.Name, l.Phone, l.Clinic, email, l.Lang, time.Now().Format(time.RFC3339)),
	}
}
