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
	subject := "Verify your switchboard account"
	body := fmt.Sprintf(`
<!DOCTYPE html>
<html>
	<body style="font-family: monospace; color: #1e1e2e; padding: 32px; max-width: 480px; margin: 0 auto;">

	<h2 style="color: #cba6f7; margin-bottom: 4px;">switchboard</h2>
	<hr style="border: none; border-top: 1px solid #e0e0e0; margin-bottom: 24px;">

	<p style="font-size: 15px; margin-bottom: 8px;">Hi,</p>
	<p style="font-size: 14px; color: #444; margin-bottom: 24px;">
		Thanks for signing up. Please verify your email address by clicking the button below.
	</p>

	<a href="%s" style="display: inline-block; background: #cba6f7; color: #1e1e2e; text-decoration: none;
		font-weight: bold; padding: 12px 28px; border-radius: 8px; font-size: 14px;">
		Verify Email
	</a>

	<p style="margin-top: 24px; font-size: 12px; color: #999;">
		Or copy and paste this link into your browser:<br>
		<a href="%s" style="color: #89b4fa;">%s</a>
	</p>

	<hr style="border: none; border-top: 1px solid #e0e0e0; margin-top: 32px;">
	<p style="font-size: 11px; color: #aaa;">
		This link expires in 24 hours. If you did not create an account, you can safely ignore this email.
	</p>

	</body>
</html>`, verifyURL, verifyURL, verifyURL)

	return m.send(to, subject, body)
}

func (m *Mailer) SendPasswordResetEmail(to, resetURL string) error {
	subject := "Reset your switchboard password"
	body := fmt.Sprintf(`
<!DOCTYPE html>
<html>
	<body style="font-family: monospace; color: #1e1e2e; padding: 32px; max-width: 480px; margin: 0 auto;">

	<h2 style="color: #cba6f7; margin-bottom: 4px;">switchboard</h2>
	<hr style="border: none; border-top: 1px solid #e0e0e0; margin-bottom: 24px;">

	<p style="font-size: 15px; margin-bottom: 8px;">Hi,</p>
	<p style="font-size: 14px; color: #444; margin-bottom: 24px;">
		We received a request to reset your password. Click the button below to continue.
	</p>

	<a href="%s" style="display: inline-block; background: #cba6f7; color: #1e1e2e; text-decoration: none;
		font-weight: bold; padding: 12px 28px; border-radius: 8px; font-size: 14px;">
		Reset Password
	</a>

	<p style="margin-top: 24px; font-size: 12px; color: #999;">
		Or copy and paste this link into your browser:<br>
		<a href="%s" style="color: #89b4fa;">%s</a>
	</p>

	<hr style="border: none; border-top: 1px solid #e0e0e0; margin-top: 32px;">
	<p style="font-size: 11px; color: #aaa;">
		This link expires in 1 hour. If you did not request a password reset, you can safely ignore this email.
	</p>

	</body>
</html>`, resetURL, resetURL, resetURL)

	return m.send(to, subject, body)
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
