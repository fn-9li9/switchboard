package mailer

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
	"time"

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
	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"></head>
<body style="background:#1e1e2e;color:#cdd6f4;font-family:'Albert Sans',sans-serif;padding:40px 20px;margin:0">
  <div style="max-width:480px;margin:0 auto;background:#181825;border:1px solid #313244;border-radius:16px;padding:32px">
    <div style="text-align:center;margin-bottom:24px">
      <h1 style="color:#cba6f7;font-size:20px;margin:12px 0 4px">Verify your email</h1>
      <p style="color:#6c7086;font-size:13px;margin:0;font-family:monospace">switchboard</p>
    </div>
    <p style="color:#cdd6f4;font-size:14px;line-height:1.6;margin:0 0 20px">
      Thanks for signing up. Please verify your email address to activate your account.
    </p>
    <div style="text-align:center;margin:24px 0">
      <a href="%s"
         style="display:inline-block;background:#cba6f7;color:#1e1e2e;text-decoration:none;
                font-weight:700;padding:13px 32px;border-radius:10px;font-size:14px;font-family:monospace">
        Verify Email →
      </a>
    </div>
    <div style="background:#1e1e2e;border:1px solid #313244;border-radius:10px;padding:14px 16px;margin:20px 0">
      <p style="color:#45475a;font-size:11px;font-family:monospace;margin:0 0 6px">or copy this link:</p>
      <a href="%s" style="color:#89b4fa;font-size:11px;font-family:monospace;word-break:break-all">%s</a>
    </div>
    <p style="color:#45475a;font-size:11px;font-family:monospace;line-height:1.6;margin:16px 0 0;text-align:center">
      This link expires in 24 hours.<br>
      If you did not create an account, you can safely ignore this email.
    </p>
  </div>
</body>
</html>`, verifyURL, verifyURL, verifyURL)

	return m.send(to, "Verify your switchboard account", body)
}

func (m *Mailer) SendPasswordResetEmail(to, resetURL string) error {
	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"></head>
<body style="background:#1e1e2e;color:#cdd6f4;font-family:'Albert Sans',sans-serif;padding:40px 20px;margin:0">
  <div style="max-width:480px;margin:0 auto;background:#181825;border:1px solid #313244;border-radius:16px;padding:32px">
    <div style="text-align:center;margin-bottom:24px">
      <h1 style="color:#89b4fa;font-size:20px;margin:12px 0 4px">Reset your password</h1>
      <p style="color:#6c7086;font-size:13px;margin:0;font-family:monospace">switchboard</p>
    </div>
    <p style="color:#cdd6f4;font-size:14px;line-height:1.6;margin:0 0 20px">
      We received a request to reset your password. Click the button below to choose a new one.
    </p>
    <div style="text-align:center;margin:24px 0">
      <a href="%s"
         style="display:inline-block;background:#89b4fa;color:#1e1e2e;text-decoration:none;
                font-weight:700;padding:13px 32px;border-radius:10px;font-size:14px;font-family:monospace">
        Reset Password →
      </a>
    </div>
    <div style="background:#1e1e2e;border:1px solid #313244;border-radius:10px;padding:14px 16px;margin:20px 0">
      <p style="color:#45475a;font-size:11px;font-family:monospace;margin:0 0 6px">or copy this link:</p>
      <a href="%s" style="color:#89b4fa;font-size:11px;font-family:monospace;word-break:break-all">%s</a>
    </div>
    <p style="color:#45475a;font-size:11px;font-family:monospace;line-height:1.6;margin:16px 0 0;text-align:center">
      This link expires in 1 hour.<br>
      If you did not request this, you can safely ignore this email.
    </p>
  </div>
</body>
</html>`, resetURL, resetURL, resetURL)

	return m.send(to, "Reset your switchboard password", body)
}

func (m *Mailer) SendMFAEnabledEmail(to string, t time.Time) error {
	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"></head>
<body style="background:#1e1e2e;color:#cdd6f4;font-family:'Albert Sans',sans-serif;padding:40px 20px;margin:0">
  <div style="max-width:480px;margin:0 auto;background:#181825;border:1px solid #313244;border-radius:16px;padding:32px">
    <div style="text-align:center;margin-bottom:24px">
      <h1 style="color:#a6e3a1;font-size:20px;margin:12px 0 4px">Two-factor auth enabled</h1>
      <p style="color:#6c7086;font-size:13px;margin:0;font-family:monospace">switchboard security alert</p>
    </div>
    <p style="color:#cdd6f4;font-size:14px;line-height:1.6">
      Two-factor authentication was <strong style="color:#a6e3a1">enabled</strong> on your account.
    </p>
    <div style="background:#1e1e2e;border:1px solid #313244;border-radius:10px;padding:16px;margin:20px 0;font-family:monospace;font-size:12px;color:#6c7086">
      <div style="margin-bottom:6px"><span style="color:#45475a">date &nbsp;</span> <span style="color:#cdd6f4">%s</span></div>
      <div><span style="color:#45475a">time &nbsp;</span> <span style="color:#cdd6f4">%s UTC</span></div>
    </div>
    <p style="color:#6c7086;font-size:12px;font-family:monospace;line-height:1.6">
      If you did not do this, your account may be compromised.<br>
      <a href="%s/auth/forgot-password" style="color:#f38ba8">Reset your password immediately</a> and contact support.
    </p>
  </div>
</body>
</html>`,
		t.UTC().Format("Monday, January 2, 2006"),
		t.UTC().Format("15:04:05"),
		m.cfg.AppURL,
	)

	return m.send(to, "Two-factor auth enabled — switchboard", body)
}

func (m *Mailer) SendMFADisabledEmail(to string, t time.Time) error {
	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"></head>
<body style="background:#1e1e2e;color:#cdd6f4;font-family:'Albert Sans',sans-serif;padding:40px 20px;margin:0">
  <div style="max-width:480px;margin:0 auto;background:#181825;border:1px solid #313244;border-radius:16px;padding:32px">
    <div style="text-align:center;margin-bottom:24px">
      <h1 style="color:#fab387;font-size:20px;margin:12px 0 4px">Two-factor auth disabled</h1>
      <p style="color:#6c7086;font-size:13px;margin:0;font-family:monospace">switchboard security alert</p>
    </div>
    <p style="color:#cdd6f4;font-size:14px;line-height:1.6">
      Two-factor authentication was <strong style="color:#fab387">disabled</strong> on your account.
    </p>
    <div style="background:#1e1e2e;border:1px solid #313244;border-radius:10px;padding:16px;margin:20px 0;font-family:monospace;font-size:12px;color:#6c7086">
      <div style="margin-bottom:6px"><span style="color:#45475a">date &nbsp;</span> <span style="color:#cdd6f4">%s</span></div>
      <div><span style="color:#45475a">time &nbsp;</span> <span style="color:#cdd6f4">%s UTC</span></div>
    </div>
    <p style="color:#6c7086;font-size:12px;font-family:monospace;line-height:1.6">
      If you did not do this, your account may be compromised.<br>
      <a href="%s/auth/forgot-password" style="color:#f38ba8">Reset your password immediately</a> and contact support.
    </p>
  </div>
</body>
</html>`,
		t.UTC().Format("Monday, January 2, 2006"),
		t.UTC().Format("15:04:05"),
		m.cfg.AppURL,
	)

	return m.send(to, "Two-factor auth disabled — switchboard", body)
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
