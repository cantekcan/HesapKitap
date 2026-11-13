// Package mail sends plain-text e-mails through an SMTP server (invoice links to customers).
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Sender sends one plain-text message.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// Config is the SMTP account. Host and From are required to send.
type Config struct {
	Host     string
	Port     string // 587 (STARTTLS) varsayılan; 465 örtük TLS
	User     string
	Password string
	From     string
}

// Enabled reports whether enough is configured to send.
func (c Config) Enabled() bool { return c.Host != "" && c.From != "" }

// SMTP implements Sender.
type SMTP struct{ cfg Config }

// NewSMTP builds the sender; it does not connect until Send.
func NewSMTP(cfg Config) *SMTP {
	if cfg.Port == "" {
		cfg.Port = "587"
	}
	return &SMTP{cfg: cfg}
}

var errBadAddress = errors.New("geçersiz e-posta adresi")

// Send delivers the message. to must be a single valid address; CR/LF in header values is rejected.
func (s *SMTP) Send(ctx context.Context, to, subject, body string) error {
	rcpt, err := mail.ParseAddress(to)
	if err != nil {
		return errBadAddress
	}
	from, err := mail.ParseAddress(s.cfg.From)
	if err != nil {
		return fmt.Errorf("gönderen adresi geçersiz: %w", err)
	}
	if strings.ContainsAny(subject, "\r\n") {
		return errors.New("konu satır sonu içeremez")
	}
	msg := strings.Join([]string{
		"From: " + from.String(),
		"To: " + rcpt.String(),
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		strings.ReplaceAll(body, "\r\n", "\n"),
	}, "\r\n")

	addr := net.JoinHostPort(s.cfg.Host, s.cfg.Port)
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	if s.cfg.Port == "465" {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("e-posta sunucusuna bağlanılamadı: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("e-posta sunucusu yanıt vermedi: %w", err)
	}
	defer func() { _ = c.Close() }()
	if ok, _ := c.Extension("STARTTLS"); ok && s.cfg.Port != "465" {
		if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("TLS başlatılamadı: %w", err)
		}
	}
	if s.cfg.User != "" {
		if err := c.Auth(smtp.PlainAuth("", s.cfg.User, s.cfg.Password, s.cfg.Host)); err != nil {
			return fmt.Errorf("e-posta girişi reddedildi: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return err
	}
	if err := c.Rcpt(rcpt.Address); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
