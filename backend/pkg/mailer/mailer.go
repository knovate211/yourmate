// Package mailer sends plain-text transactional email over SMTP.
//
// In development the stack points SMTP at Mailpit, which catches every message
// and shows it at http://localhost:8027.
package mailer

import (
	"fmt"
	"log/slog"
	"mime"
	"net/smtp"
	"strings"
	"time"
)

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

	var auth smtp.Auth
	if m.Username != "" {
		auth = smtp.PlainAuth("", m.Username, m.Password, m.Host)
	}
	addr := fmt.Sprintf("%s:%d", m.Host, m.Port)
	if err := smtp.SendMail(addr, auth, envelope(m.From), []string{to}, []byte(msg)); err != nil {
		return fmt.Errorf("send mail to %s: %w", to, err)
	}
	return nil
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
