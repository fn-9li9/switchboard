package handlers

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"switchboard/internal/auth"
	"switchboard/internal/config"
	"switchboard/internal/mailer"
	authstore "switchboard/internal/store/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

const (
	maxLoginAttemptsByEmail = 10
	maxLoginAttemptsByIP    = 20
	lockoutWindow           = 15 * time.Minute
)

type AuthHandler struct {
	pool      *pgxpool.Pool
	log       zerolog.Logger
	cfg       *config.Config
	sm        *auth.SessionManager
	enc       *auth.Encryptor
	turnstile *auth.TurnstileVerifier
	mailer    *mailer.Mailer
}

func NewAuthHandler(
	pool *pgxpool.Pool,
	log zerolog.Logger,
	cfg *config.Config,
	sm *auth.SessionManager,
	enc *auth.Encryptor,
	turnstile *auth.TurnstileVerifier,
	m *mailer.Mailer,
) *AuthHandler {
	return &AuthHandler{
		pool:      pool,
		log:       log,
		cfg:       cfg,
		sm:        sm,
		enc:       enc,
		turnstile: turnstile,
		mailer:    m,
	}
}

// ── GET /auth/sigin ────────────────────────────────────────────────

func (h *AuthHandler) ShowLogin(w http.ResponseWriter, r *http.Request) {
	h.renderAuth(w, r, "signin.html", map[string]any{
		"SiteKey": h.cfg.Turnstile.SiteKey,
		"Flash":   r.URL.Query().Get("flash"),
	})
}

// ── POST /auth/signin ───────────────────────────────────────────────

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ip := auth.ClientIP(r)
	ua := auth.UserAgent(r)

	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	password := r.FormValue("password")
	cfToken := r.FormValue("cf-turnstile-response")
	next := r.FormValue("next")
	if next == "" {
		next = "/"
	}

	// ── 1. Turnstile ──────────────────────────────────────────
	if err := h.turnstile.Verify(cfToken, ip); err != nil {
		h.log.Warn().Str("ip", ip).Msg("login: turnstile failed")
		h.recordAttempt(ctx, email, ip, ua, false, ptr("turnstile"))
		h.renderAuth(w, r, "signin.html", map[string]any{
			"SiteKey": h.cfg.Turnstile.SiteKey,
			"Error":   "Captcha verification failed. Please try again.",
			"Email":   email,
			"Next":    next,
		})
		return
	}

	// ── 2. Lockout check ─────────────────────────────────────
	byEmail, byIP, err := authstore.CountRecentFailures(ctx, h.pool, email, ip, lockoutWindow)
	if err != nil {
		h.log.Error().Err(err).Msg("login: count failures")
	}
	if byEmail >= maxLoginAttemptsByEmail || byIP >= maxLoginAttemptsByIP {
		h.log.Warn().Str("email", email).Str("ip", ip).Msg("login: account locked")
		h.recordAttempt(ctx, email, ip, ua, false, ptr("locked"))
		h.renderAuth(w, r, "signin.html", map[string]any{
			"SiteKey": h.cfg.Turnstile.SiteKey,
			"Error":   "Too many failed attempts. Please wait 15 minutes.",
			"Email":   email,
			"Next":    next,
		})
		return
	}

	// ── 3. Buscar usuario ─────────────────────────────────────
	user, err := authstore.GetUserByEmail(ctx, h.pool, email)
	if err != nil {
		// No revelar si el email existe o no
		h.recordAttempt(ctx, email, ip, ua, false, ptr("bad_password"))
		h.renderAuth(w, r, "signin.html", map[string]any{
			"SiteKey": h.cfg.Turnstile.SiteKey,
			"Error":   "Invalid email or password.",
			"Email":   email,
			"Next":    next,
		})
		return
	}

	if !user.IsActive {
		h.recordAttempt(ctx, email, ip, ua, false, ptr("locked"))
		h.renderAuth(w, r, "signin.html", map[string]any{
			"SiteKey": h.cfg.Turnstile.SiteKey,
			"Error":   "This account has been disabled.",
			"Email":   email,
			"Next":    next,
		})
		return
	}

	// ── 4. Verificar password ─────────────────────────────────
	if user.PasswordHash == nil {
		// Usuario solo OAuth — no tiene password
		h.renderAuth(w, r, "signin.html", map[string]any{
			"SiteKey": h.cfg.Turnstile.SiteKey,
			"Error":   "This account uses Google sign-in. Please use that option.",
			"Email":   email,
			"Next":    next,
		})
		return
	}

	fmt.Printf("DEBUG Login: password_len=%d hash_preview=%.40s\n", len(password), *user.PasswordHash)
	if err := auth.VerifyPassword(password, *user.PasswordHash); err != nil {
		fmt.Printf("DEBUG Login: VerifyPassword error: %v\n", err)
		h.recordAttempt(ctx, email, ip, ua, false, ptr("bad_password"))
		h.renderAuth(w, r, "signin.html", map[string]any{
			"SiteKey": h.cfg.Turnstile.SiteKey,
			"Error":   "Invalid email or password.",
			"Email":   email,
			"Next":    next,
		})
		return
	}

	// ── 5. Email verificado ───────────────────────────────────
	if !user.EmailVerified {
		h.recordAttempt(ctx, email, ip, ua, false, ptr("unverified"))
		h.renderAuth(w, r, "signin.html", map[string]any{
			"SiteKey":    h.cfg.Turnstile.SiteKey,
			"Error":      "Please verify your email before logging in.",
			"Email":      email,
			"Next":       next,
			"ShowResend": true,
		})
		return
	}

	// ── 6. Crear sesión ───────────────────────────────────────
	sessionID, err := auth.GenerateRandomHex(32)
	if err != nil {
		h.log.Error().Err(err).Msg("login: generate session id")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	mfaVerified := !user.MFAEnabled // si no tiene MFA, la sesión ya está "verificada"

	session := authstore.Session{
		ID:          sessionID,
		UserID:      user.ID,
		IPAddress:   &ip,
		UserAgent:   &ua,
		MFAVerified: mfaVerified,
		ExpiresAt:   time.Now().Add(h.cfg.Auth.SessionDuration),
	}

	if err := authstore.CreateSession(ctx, h.pool, session); err != nil {
		h.log.Error().Err(err).Msg("login: create session")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// ── 7. Registrar éxito ────────────────────────────────────
	h.recordAttempt(ctx, email, ip, ua, true, nil)
	authstore.UpdateLastLogin(ctx, h.pool, user.ID)
	h.auditLog(ctx, &user.ID, "login", ip, ua, map[string]any{
		"session_id": sessionID,
	})

	h.sm.SetCookie(w, sessionID)

	// ── 8. Redirigir ──────────────────────────────────────────
	if user.MFAEnabled {
		http.Redirect(w, r, "/mfa/verify?next="+next, http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, next, http.StatusSeeOther)
}

// ── GET /auth/signup ─────────────────────────────────────────────

func (h *AuthHandler) ShowRegister(w http.ResponseWriter, r *http.Request) {
	h.renderAuth(w, r, "signup.html", map[string]any{
		"SiteKey": h.cfg.Turnstile.SiteKey,
	})
}

// ── POST /auth/signup ────────────────────────────────────────────

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ip := auth.ClientIP(r)
	ua := auth.UserAgent(r)

	firstName := strings.TrimSpace(r.FormValue("first_name"))
	lastName := strings.TrimSpace(r.FormValue("last_name"))
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	password := r.FormValue("password")
	confirm := r.FormValue("password_confirm")
	cfToken := r.FormValue("cf-turnstile-response")
	displayName := strings.TrimSpace(firstName + " " + lastName)

	// ── 1. Validaciones básicas ───────────────────────────────
	if email == "" || password == "" {
		h.renderAuth(w, r, "signup.html", map[string]any{
			"SiteKey": h.cfg.Turnstile.SiteKey,
			"Error":   "Email and password are required.",
			"Email":   email,
		})
		return
	}

	if !isValidEmail(email) {
		h.renderAuth(w, r, "signup.html", map[string]any{
			"SiteKey": h.cfg.Turnstile.SiteKey,
			"Error":   "Invalid email address.",
			"Email":   email,
		})
		return
	}

	if len(password) < 8 {
		h.renderAuth(w, r, "signup.html", map[string]any{
			"SiteKey": h.cfg.Turnstile.SiteKey,
			"Error":   "Password must be at least 8 characters.",
			"Email":   email,
		})
		return
	}

	if len(password) > 72 {
		h.renderAuth(w, r, "signup.html", map[string]any{
			"SiteKey": h.cfg.Turnstile.SiteKey,
			"Error":   "Password must be at most 72 characters.",
			"Email":   email,
		})
		return
	}

	if password != confirm {
		h.renderAuth(w, r, "signup.html", map[string]any{
			"SiteKey": h.cfg.Turnstile.SiteKey,
			"Error":   "Passwords do not match.",
			"Email":   email,
		})
		return
	}

	// ── 2. Turnstile ──────────────────────────────────────────
	if err := h.turnstile.Verify(cfToken, ip); err != nil {
		h.log.Warn().Str("ip", ip).Msg("register: turnstile failed")
		h.renderAuth(w, r, "signup.html", map[string]any{
			"SiteKey": h.cfg.Turnstile.SiteKey,
			"Error":   "Captcha verification failed. Please try again.",
			"Email":   email,
		})
		return
	}

	// ── 3. Hash password ──────────────────────────────────────
	hash, err := auth.HashPassword(password)
	if err != nil {
		h.log.Error().Err(err).Msg("register: hash password")
		h.renderAuth(w, r, "signup.html", map[string]any{
			"SiteKey": h.cfg.Turnstile.SiteKey,
			"Error":   err.Error(),
			"Email":   email,
		})
		return
	}

	// ── 4. Crear usuario ──────────────────────────────────────
	userID, err := authstore.CreateUser(ctx, h.pool, email, hash, displayName)
	if err != nil {
		if err == authstore.ErrDuplicate {
			h.renderAuth(w, r, "signup.html", map[string]any{
				"SiteKey": h.cfg.Turnstile.SiteKey,
				"Error":   "An account with this email already exists.",
				"Email":   email,
			})
			return
		}
		h.log.Error().Err(err).Msg("register: create user")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// ── 5. Generar token de verificación ──────────────────────
	token, err := auth.GenerateRandomHex(32)
	if err != nil {
		h.log.Error().Err(err).Msg("register: generate token")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := authstore.CreateVerificationToken(
		ctx, h.pool, userID, token, "email_verify",
		time.Now().Add(24*time.Hour),
	); err != nil {
		h.log.Error().Err(err).Msg("register: create verification token")
	}

	// ── 6. Enviar email ───────────────────────────────────────
	verifyURL := fmt.Sprintf("%s/auth/verify-email?token=%s", h.cfg.App.URL, token)
	if h.mailer != nil {
		if err := h.mailer.SendVerificationEmail(email, verifyURL); err != nil {
			h.log.Warn().Err(err).Str("email", email).Msg("register: send email failed")
		}
	} else {
		h.log.Info().Str("verify_url", verifyURL).Msg("register: verification URL (dev mode)")
	}

	// ── 7. Audit log ──────────────────────────────────────────
	h.auditLog(ctx, &userID, "register", ip, ua, map[string]any{
		"email":        email,
		"display_name": displayName,
	})

	// ── 8. Redirect ───────────────────────────────────────────
	http.Redirect(w, r, "/auth/verify-email/pending?email="+email, http.StatusSeeOther)
}

// ── GET /auth/verify-email ─────────────────────────────────────────

func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := r.URL.Query().Get("token")

	if token == "" {
		h.renderAuth(w, r, "verify.html", map[string]any{
			"Error": "Invalid verification link.",
		})
		return
	}

	vt, err := authstore.GetVerificationToken(ctx, h.pool, token)
	if err != nil {
		h.renderAuth(w, r, "verify.html", map[string]any{
			"Error": "This link is invalid or has expired.",
		})
		return
	}

	if vt.UsedAt != nil {
		h.renderAuth(w, r, "verify.html", map[string]any{
			"Error": "This link has already been used.",
		})
		return
	}

	if time.Now().After(vt.ExpiresAt) {
		h.renderAuth(w, r, "verify.html", map[string]any{
			"Error": "This link has expired. Please register again.",
		})
		return
	}

	// Marcar token como usado y verificar email en una transacción
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		h.log.Error().Err(err).Msg("verify email: begin tx")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	if err := authstore.UseVerificationToken(ctx, h.pool, token); err != nil {
		h.log.Error().Err(err).Msg("verify email: use token")
		h.renderAuth(w, r, "verify.html", map[string]any{
			"Error": "Could not verify email. Please try again.",
		})
		return
	}

	if err := authstore.VerifyUserEmail(ctx, h.pool, vt.UserID); err != nil {
		h.log.Error().Err(err).Msg("verify email: update user")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	tx.Commit(ctx)

	ip := auth.ClientIP(r)
	ua := auth.UserAgent(r)
	h.auditLog(ctx, &vt.UserID, "email_verify", ip, ua, nil)

	h.renderAuth(w, r, "verify.html", map[string]any{
		"Success": true,
	})
}

// ── GET /verify-email/pending ─────────────────────────────────

func (h *AuthHandler) VerifyEmailPending(w http.ResponseWriter, r *http.Request) {
	h.renderAuth(w, r, "verify-pending.html", map[string]any{
		"Email": r.URL.Query().Get("email"),
	})
}

// ── POST /logout ──────────────────────────────────────────────

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sessionID, err := h.sm.ParseCookie(r)
	if err == nil {
		authstore.RevokeSession(ctx, h.pool, sessionID)

		session := auth.SessionFromContext(ctx)
		if session != nil {
			ip := auth.ClientIP(r)
			ua := auth.UserAgent(r)
			h.auditLog(ctx, &session.UserID, "logout", ip, ua, map[string]any{
				"session_id": sessionID,
			})
		}
	}

	h.sm.ClearCookie(w)
	http.Redirect(w, r, "/auth/signin", http.StatusSeeOther)
}

// ── GET /auth/resend-verification ────────────────────────────
func (h *AuthHandler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))

	if email == "" {
		h.renderAuth(w, r, "signin.html", map[string]any{
			"SiteKey": h.cfg.Turnstile.SiteKey,
			"Error":   "Email is required to resend verification.",
		})
		return
	}

	user, err := authstore.GetUserByEmail(ctx, h.pool, email)
	if err != nil || user.EmailVerified {
		// No revelar si existe o no
		http.Redirect(w, r, "/auth/verify-email/pending?email="+email, http.StatusSeeOther)
		return
	}

	token, err := auth.GenerateRandomHex(32)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := authstore.CreateVerificationToken(
		ctx, h.pool, user.ID, token, "email_verify",
		time.Now().Add(24*time.Hour),
	); err != nil {
		h.log.Error().Err(err).Msg("resend: create token")
	}

	verifyURL := fmt.Sprintf("%s/auth/verify-email?token=%s", h.cfg.App.URL, token)
	if h.mailer != nil {
		h.mailer.SendVerificationEmail(email, verifyURL)
	} else {
		h.log.Info().Str("verify_url", verifyURL).Msg("resend: verification URL (dev mode)")
	}

	http.Redirect(w, r, "/auth/verify-email/pending?email="+email, http.StatusSeeOther)
}

// ── GET /auth/forgot-password ─────────────────────────────────
func (h *AuthHandler) ShowForgotPassword(w http.ResponseWriter, r *http.Request) {
	h.renderAuth(w, r, "forgot-password.html", map[string]any{
		"SiteKey": h.cfg.Turnstile.SiteKey,
		"Sent":    r.URL.Query().Get("sent") == "1",
		"Email":   r.URL.Query().Get("email"),
	})
}

// ── POST /auth/forgot-password ────────────────────────────────
func (h *AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))

	// Siempre redirigir con sent=1 para no revelar si el email existe
	redirect := func() {
		http.Redirect(w, r, "/auth/forgot-password?sent=1&email="+email, http.StatusSeeOther)
	}

	user, err := authstore.GetUserByEmail(ctx, h.pool, email)
	if err != nil {
		// No revelar que el email no existe
		redirect()
		return
	}

	if !user.IsActive {
		redirect()
		return
	}

	token, err := auth.GenerateRandomHex(32)
	if err != nil {
		h.log.Error().Err(err).Msg("forgot-password: generate token")
		redirect()
		return
	}

	if err := authstore.CreateVerificationToken(ctx, h.pool, user.ID, token, "password_reset",
		time.Now().Add(1*time.Hour),
	); err != nil {
		h.log.Error().Err(err).Msg("forgot-password: create token")
		redirect()
		return
	}

	resetURL := h.cfg.App.URL + "/auth/reset-password?token=" + token

	h.log.Info().Str("reset_url", resetURL).Msg("forgot-password: sending reset email")

	if err := h.mailer.SendPasswordResetEmail(user.Email, resetURL); err != nil {
		h.log.Error().Err(err).Msg("forgot-password: send email")
	}

	go authstore.InsertAuditLog(context.Background(), h.pool, &user.ID, "password_reset_request",
		auth.ClientIP(r), auth.UserAgent(r), map[string]any{"email": email})

	redirect()
}

// ── GET /auth/reset-password ──────────────────────────────────
func (h *AuthHandler) ShowResetPassword(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Redirect(w, r, "/auth/forgot-password", http.StatusSeeOther)
		return
	}

	// Verificar que el token existe y no está expirado (sin marcarlo como usado)
	ctx := r.Context()
	vt, err := authstore.GetVerificationToken(ctx, h.pool, token)
	if err != nil || vt.TokenType != "password_reset" || vt.UsedAt != nil || time.Now().After(vt.ExpiresAt) {
		h.renderAuth(w, r, "reset-password.html", map[string]any{
			"Invalid": true,
		})
		return
	}

	h.renderAuth(w, r, "reset-password.html", map[string]any{
		"Token":   token,
		"Invalid": false,
	})
}

// ── POST /auth/reset-password ─────────────────────────────────
func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := r.FormValue("token")
	newPw := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")

	renderErr := func(msg string) {
		h.renderAuth(w, r, "reset-password.html", map[string]any{
			"Token":   token,
			"Invalid": false,
			"Error":   msg,
		})
	}

	if newPw != confirm {
		renderErr("Passwords do not match.")
		return
	}
	if len(newPw) < 8 {
		renderErr("Password must be at least 8 characters.")
		return
	}

	// Validar token
	vt, err := authstore.GetVerificationToken(ctx, h.pool, token)
	if err != nil || vt.TokenType != "password_reset" || vt.UsedAt != nil || time.Now().After(vt.ExpiresAt) {
		h.renderAuth(w, r, "reset-password.html", map[string]any{"Invalid": true})
		return
	}

	// Hashear nueva contraseña
	hash, err := auth.HashPassword(newPw)
	if err != nil {
		renderErr("Could not process password. Please try again.")
		return
	}

	// Marcar token como usado
	if err := authstore.UseVerificationToken(ctx, h.pool, token); err != nil {
		h.renderAuth(w, r, "reset-password.html", map[string]any{"Invalid": true})
		return
	}

	// Guardar nueva contraseña y revocar todas las sesiones
	authstore.UpdatePassword(ctx, h.pool, vt.UserID, hash)
	authstore.RevokeAllUserSessions(ctx, h.pool, vt.UserID, "")

	go authstore.InsertAuditLog(context.Background(), h.pool, &vt.UserID, "password_reset",
		auth.ClientIP(r), auth.UserAgent(r), nil)

	http.Redirect(w, r, "/auth/signin?flash=password_reset", http.StatusSeeOther)
}

// ── Helpers ───────────────────────────────────────────────────

func (h *AuthHandler) renderAuth(w http.ResponseWriter, r *http.Request, tmplFile string, data map[string]any) {
	tmpl, err := template.ParseFiles(
		"services/gateway/templates/auth/base.html",
		"services/gateway/templates/auth/"+tmplFile,
	)
	if err != nil {
		h.log.Error().Err(err).Str("template", tmplFile).Msg("auth: parse template")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.ExecuteTemplate(w, "base.html", data); err != nil {
		h.log.Error().Err(err).Msg("auth: execute template")
	}
}

func (h *AuthHandler) recordAttempt(
	ctx context.Context,
	email, ip, ua string,
	success bool,
	reason *string,
) {
	go func() {
		ctx2, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := authstore.RecordLoginAttempt(ctx2, h.pool, email, ip, ua, success, reason); err != nil {
			h.log.Warn().Err(err).Msg("auth: record attempt failed")
		}
	}()
}

func (h *AuthHandler) auditLog(
	ctx context.Context,
	userID *uuid.UUID,
	action, ip, ua string,
	metadata map[string]any,
) {
	go func() {
		ctx2, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := authstore.InsertAuditLog(ctx2, h.pool, userID, action, ip, ua, metadata); err != nil {
			h.log.Warn().Err(err).Str("action", action).Msg("audit log failed")
		}
	}()
}

func isValidEmail(email string) bool {
	at := strings.Index(email, "@")
	if at < 1 {
		return false
	}
	dot := strings.LastIndex(email[at:], ".")
	return dot > 1 && at+dot < len(email)-1
}

func ptr(s string) *string { return &s }
