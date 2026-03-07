package mailer

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"

	"switchboard/internal/config"

	"github.com/rs/zerolog"
)

type Mailer struct {
	cfg config.SMTPConfig
	log zerolog.Logger
}

func New(cfg config.SMTPConfig, log zerolog.Logger) *Mailer {
	return &Mailer{cfg: cfg, log: log}
}

func (m *Mailer) SendVerificationEmail(to, verifyURL string) error {
	subject := "Verify your switchboard account"
	body := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<body style="font-family: monospace; background: #1e1e2e; color: #cdd6f4; padding: 32px;">
  <h2 style="color: #cba6f7;">⚡ switchboard</h2>
  <p>Click the link below to verify your email address:</p>
  <p><a href="%s" style="color: #89b4fa;">%s</a></p>
  <p style="color: #6c7086; font-size: 12px;">This link expires in 24 hours.</p>
</body>
</html>`, verifyURL, verifyURL)

	return m.send(to, subject, body)
}

func (m *Mailer) SendPasswordResetEmail(to, resetURL string) error {
	subject := "Reset your switchboard password"
	body := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<body style="font-family: monospace; background: #1e1e2e; color: #cdd6f4; padding: 32px;">
  <h2 style="color: #cba6f7;">⚡ switchboard</h2>
  <p>Click the link below to reset your password:</p>
  <p><a href="%s" style="color: #89b4fa;">%s</a></p>
  <p style="color: #6c7086; font-size: 12px;">This link expires in 1 hour. If you did not request this, ignore this email.</p>
</body>
</html>`, resetURL, resetURL)

	return m.send(to, subject, body)
}

func (m *Mailer) send(to, subject, htmlBody string) error {
	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)

	msg := strings.Join([]string{
		"From: " + m.cfg.From,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"",
		htmlBody,
	}, "\r\n")

	auth := smtp.PlainAuth("", m.cfg.User, m.cfg.Password, m.cfg.Host)

	tlsCfg := &tls.Config{
		InsecureSkipVerify: false,
		ServerName:         m.cfg.Host,
	}

	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		// Fallback a STARTTLS en puerto 587
		return smtp.SendMail(addr, auth, m.cfg.From, []string{to}, []byte(msg))
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(m.cfg.From); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	wc, err := client.Data()
	if err != nil {
		return err
	}
	defer wc.Close()
	_, err = wc.Write([]byte(msg))
	return err
}
