// Package mailer sends plain-text transactional email over SMTP.
//
// In development the stack points SMTP at Mailpit, which catches every message
// and shows it at http://localhost:8027. In production it talks to a real
// provider (Gmail, SES, Brevo…) on port 587: the connection is upgraded with
// STARTTLS before the password is sent.
package mailer

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// timeout bounds a whole delivery, so a slow or unreachable provider can't
// hang the request that sends a signup code.
const timeout = 20 * time.Second

// Mailer sends messages. A zero Host turns sending into logging only.
type Mailer struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// Send delivers one message. Failures are returned so callers can decide
// whether the action that triggered the email should fail too.
func (m *Mailer) Send(to, subject, body string) error {
	if m.Host == "" {
		slog.Info("email (smtp disabled)", "to", to, "subject", subject, "body", body)
		return nil
	}
	msg := strings.Join([]string{
		"From: " + m.From,
		"To: " + to,
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		body,
	}, "\r\n")

	if err := m.deliver(to, []byte(msg)); err != nil {
		return fmt.Errorf("send mail to %s: %w", to, err)
	}
	return nil
}

func (m *Mailer) deliver(to string, msg []byte) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(m.Host, fmt.Sprint(m.Port)), timeout)
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	c, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: m.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if m.Username != "" {
		// PlainAuth refuses to send the password over an unencrypted
		// connection (except to localhost), so a provider without STARTTLS
		// fails here instead of leaking it.
		if ok, _ := c.Extension("AUTH"); !ok {
			return errors.New("server does not accept a login (AUTH); check SMTP_HOST and SMTP_PORT")
		}
		if err := c.Auth(smtp.PlainAuth("", m.Username, m.Password, m.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(envelope(m.From)); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// SendAsync sends in the background and only logs failures. Use it for
// notifications whose loss must not fail the user's action.
func (m *Mailer) SendAsync(to, subject, body string) {
	go func() {
		if err := m.Send(to, subject, body); err != nil {
			slog.Warn("async email failed", "err", err)
		}
	}()
}

// envelope extracts the bare address from "Name <addr>".
func envelope(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		return strings.TrimSuffix(from[i+1:], ">")
	}
	return from
}
