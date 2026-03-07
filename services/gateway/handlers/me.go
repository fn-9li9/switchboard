package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"switchboard/internal/auth"
	"switchboard/internal/config"
	authstore "switchboard/internal/store/auth"
)

type MeHandler struct {
	pool *pgxpool.Pool
	log  zerolog.Logger
	cfg  *config.Config
	sm   *auth.SessionManager
	enc  *auth.Encryptor
}

func NewMeHandler(pool *pgxpool.Pool, log zerolog.Logger, cfg *config.Config, sm *auth.SessionManager, enc *auth.Encryptor) *MeHandler {
	return &MeHandler{pool: pool, log: log, cfg: cfg, sm: sm, enc: enc}
}

// SecurityData es el struct que se pasa a security.html
type SecurityData struct {
	HasPassword bool
}

type SetupData struct {
	QRCode string
	Secret string
	Error  string
}

// ── Shared data para todas las vistas /me ─────────────────────

type MePageData struct {
	User      *NavUser
	ActiveTab string
	Flash     string
	FlashType string // "success" | "error"
	Data      any
}

func (h *MeHandler) meData(r *http.Request, tab string, navUser *NavUser) *MePageData {
	return &MePageData{
		User:      navUser,
		ActiveTab: tab,
	}
}

// ── GET /me/profile ───────────────────────────────────────────

func (h *MeHandler) Profile(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		user, err := authstore.GetUserByID(ctx, h.pool, mustParseUUID(navUser.ID))
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		data := h.meData(r, "profile", navUser)
		data.Data = user
		h.render(w, "profile.html", data)
	}
}

// ── POST /me/profile ──────────────────────────────────────────

func (h *MeHandler) UpdateProfile(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID := mustParseUUID(navUser.ID)

		displayName := r.FormValue("display_name")
		avatarURL := r.FormValue("avatar_url")

		if displayName != "" {
			h.pool.Exec(ctx, `UPDATE auth.users SET display_name=$2, updated_at=NOW() WHERE id=$1`, userID, displayName)
		}
		if avatarURL != "" {
			authstore.UpdateAvatar(ctx, h.pool, userID, avatarURL)
		}

		http.Redirect(w, r, "/me/profile?flash=saved", http.StatusSeeOther)
	}
}

// ── GET /me/security ──────────────────────────────────────────

func (h *MeHandler) Security(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user, err := authstore.GetUserByID(ctx, h.pool, mustParseUUID(navUser.ID))
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		data := h.meData(r, "security", navUser)
		data.Flash = r.URL.Query().Get("flash")
		data.FlashType = r.URL.Query().Get("type")
		data.Data = SecurityData{HasPassword: user.PasswordHash != nil}
		h.render(w, "security.html", data)
	}
}

// ── POST /me/security/password ────────────────────────────────

func (h *MeHandler) ChangePassword(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID := mustParseUUID(navUser.ID)
		isSet := r.FormValue("is_set") == "1"

		newPw := r.FormValue("new_password")
		confirm := r.FormValue("confirm_password")

		fmt.Printf("DEBUG ChangePassword: isSet=%v newPw_len=%d confirm_len=%d\n", isSet, len(newPw), len(confirm))

		if newPw != confirm {
			fmt.Printf("DEBUG: mismatch\n")
			http.Redirect(w, r, "/me/security?flash=mismatch&type=error", http.StatusSeeOther)
			return
		}
		if len(newPw) < 8 {
			fmt.Printf("DEBUG: too_short\n")
			http.Redirect(w, r, "/me/security?flash=too_short&type=error", http.StatusSeeOther)
			return
		}

		user, err := authstore.GetUserByID(ctx, h.pool, userID)
		if err != nil {
			fmt.Printf("DEBUG: GetUserByID error: %v\n", err)
			http.Redirect(w, r, "/me/security?flash=error&type=error", http.StatusSeeOther)
			return
		}

		fmt.Printf("DEBUG: user.PasswordHash nil=%v isSet=%v\n", user.PasswordHash == nil, isSet)

		if user.PasswordHash != nil && !isSet {
			current := r.FormValue("current_password")
			fmt.Printf("DEBUG: current_len=%d hash_preview=%.40s\n", len(current), *user.PasswordHash)
			if err := auth.VerifyPassword(current, *user.PasswordHash); err != nil {
				fmt.Printf("DEBUG: VerifyPassword error: %v\n", err)
				http.Redirect(w, r, "/me/security?flash=wrong_password&type=error", http.StatusSeeOther)
				return
			}
		}

		hash, err := auth.HashPassword(newPw)
		if err != nil {
			http.Redirect(w, r, "/me/security?flash=error&type=error", http.StatusSeeOther)
			return
		}

		authstore.UpdatePassword(ctx, h.pool, userID, hash)

		// Solo revocar otras sesiones si es cambio, no si es set inicial
		if !isSet {
			sessionID, _ := h.sm.ParseCookie(r)
			authstore.RevokeAllUserSessions(ctx, h.pool, userID, sessionID)
		}

		ip := auth.ClientIP(r)
		ua := auth.UserAgent(r)
		go authstore.InsertAuditLog(context.Background(), h.pool, &userID, "password_change", ip, ua, map[string]any{"is_set": isSet})

		flash := "password_changed"
		if isSet {
			flash = "password_set"
		}
		http.Redirect(w, r, "/me/security?flash="+flash+"&type=success", http.StatusSeeOther)
	}
}

// ── POST /me/security/delete ──────────────────────────────────

func (h *MeHandler) DeleteAccount(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID := mustParseUUID(navUser.ID)

		confirm := r.FormValue("confirm")
		if confirm != navUser.Email {
			http.Redirect(w, r, "/me/security?flash=confirm_mismatch&type=error", http.StatusSeeOther)
			return
		}

		// Desactivar en lugar de DELETE para preservar audit log
		h.pool.Exec(ctx, `UPDATE auth.users SET is_active=false, updated_at=NOW() WHERE id=$1`, userID)
		authstore.RevokeAllUserSessions(ctx, h.pool, userID, "")

		ip := auth.ClientIP(r)
		ua := auth.UserAgent(r)
		go authstore.InsertAuditLog(context.Background(), h.pool, &userID, "account_lock", ip, ua, map[string]any{"reason": "self_delete"})

		h.sm.ClearCookie(w)
		http.Redirect(w, r, "/auth/signin?flash=account_deleted", http.StatusSeeOther)
	}
}

// ── GET /me/sessions ──────────────────────────────────────────

func (h *MeHandler) Sessions(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID := mustParseUUID(navUser.ID)

		currentSessionID, _ := h.sm.ParseCookie(r)

		sessions, err := authstore.ListUserSessions(ctx, h.pool, userID)
		if err != nil {
			h.log.Error().Err(err).Msg("me/sessions: list")
			sessions = nil
		}

		type SessionRow struct {
			authstore.Session
			IsCurrent bool
		}
		var rows []SessionRow
		for _, s := range sessions {
			rows = append(rows, SessionRow{
				Session:   s,
				IsCurrent: s.ID == currentSessionID,
			})
		}

		data := h.meData(r, "sessions", navUser)
		data.Flash = r.URL.Query().Get("flash")
		data.FlashType = r.URL.Query().Get("type")
		data.Data = rows
		h.render(w, "sessions.html", data)
	}
}

// ── POST /me/sessions/revoke ──────────────────────────────────

func (h *MeHandler) RevokeSession(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID := mustParseUUID(navUser.ID)
		sessionID := r.FormValue("session_id")

		// Verificar que la sesión pertenece al usuario
		session, err := authstore.GetSession(ctx, h.pool, sessionID)
		if err != nil || session.UserID != userID {
			http.Redirect(w, r, "/me/sessions?flash=not_found&type=error", http.StatusSeeOther)
			return
		}

		authstore.RevokeSession(ctx, h.pool, sessionID)
		http.Redirect(w, r, "/me/sessions?flash=revoked&type=success", http.StatusSeeOther)
	}
}

// ── POST /me/sessions/revoke-all ─────────────────────────────

func (h *MeHandler) RevokeAllSessions(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID := mustParseUUID(navUser.ID)

		currentSessionID, _ := h.sm.ParseCookie(r)
		authstore.RevokeAllUserSessions(ctx, h.pool, userID, currentSessionID)

		http.Redirect(w, r, "/me/sessions?flash=all_revoked&type=success", http.StatusSeeOther)
	}
}

// ── GET /me/mfa ───────────────────────────────────────────────

func (h *MeHandler) MFA(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := h.meData(r, "mfa", navUser)
		data.Flash = r.URL.Query().Get("flash")
		data.FlashType = r.URL.Query().Get("type")
		h.render(w, "mfa.html", data)
	}
}

// ── GET /me/mfa/setup ─────────────────────────────────────────

func (h *MeHandler) MFASetup(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID := mustParseUUID(navUser.ID)

		// Si ya tiene MFA activo, redirigir
		user, err := authstore.GetUserByID(ctx, h.pool, userID)
		if err != nil || user.MFAEnabled {
			http.Redirect(w, r, "/me/mfa", http.StatusSeeOther)
			return
		}

		// Generar secret + QR
		key, qrBase64, err := auth.GenerateTOTPSecret(h.cfg.Auth.MFAIssuer, navUser.Email)
		if err != nil {
			h.log.Error().Err(err).Msg("mfa/setup: generate secret")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Encriptar secret y guardarlo temporalmente (aún no activado)
		encryptedSecret, err := h.enc.Encrypt(key.Secret())
		if err != nil {
			h.log.Error().Err(err).Msg("mfa/setup: encrypt secret")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		authstore.UpdateMFASecret(ctx, h.pool, userID, encryptedSecret)

		data := h.meData(r, "mfa", navUser)
		data.Data = SetupData{
			QRCode: qrBase64,
			Secret: key.Secret(),
		}
		h.render(w, "mfa-setup.html", data)
	}
}

// ── POST /me/mfa/setup ────────────────────────────────────────

func (h *MeHandler) MFASetupConfirm(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID := mustParseUUID(navUser.ID)
		code := strings.TrimSpace(r.FormValue("code"))

		user, err := authstore.GetUserByID(ctx, h.pool, userID)
		if err != nil || user.MFASecret == nil {
			http.Redirect(w, r, "/me/mfa/setup", http.StatusSeeOther)
			return
		}

		// Desencriptar secret
		secret, err := h.enc.Decrypt(*user.MFASecret)
		if err != nil {
			h.log.Error().Err(err).Msg("mfa/setup: decrypt secret")
			http.Redirect(w, r, "/me/mfa/setup?error=decrypt", http.StatusSeeOther)
			return
		}

		// Verificar código
		if !auth.ValidateTOTP(code, secret) {
			// Re-renderizar setup con error
			key, qrBase64, _ := auth.GenerateTOTPSecret(h.cfg.Auth.MFAIssuer, navUser.Email)
			if key != nil {
				enc, _ := h.enc.Encrypt(key.Secret())
				authstore.UpdateMFASecret(ctx, h.pool, userID, enc)
			}

			data := h.meData(r, "mfa", navUser)
			data.Data = SetupData{
				QRCode: qrBase64,
				Secret: func() string {
					if key != nil {
						return key.Secret()
					}
					return ""
				}(),
				Error: "Invalid code. Please try again.",
			}
			h.render(w, "mfa-setup.html", data)
			return
		}

		// Activar MFA
		authstore.EnableMFA(ctx, h.pool, userID)

		// Generar 10 backup codes
		backupCodes, plainCodes, err := h.generateBackupCodes(ctx, userID)
		if err != nil {
			h.log.Error().Err(err).Msg("mfa/setup: generate backup codes")
			http.Redirect(w, r, "/me/mfa?flash=mfa_enabled&type=success", http.StatusSeeOther)
			return
		}
		_ = backupCodes

		// Guardar en sesión temporal via cookie firmada para mostrar UNA sola vez
		// Usamos un parámetro de query firmado — los códigos ya están en DB encriptados
		// Los mostramos desde la DB desencriptados

		ip := auth.ClientIP(r)
		ua := auth.UserAgent(r)
		go authstore.InsertAuditLog(context.Background(), h.pool, &userID, "mfa_enabled", ip, ua, nil)

		// Guardar códigos en cookie temporal (base64) para mostrarlos una sola vez
		codesJSON, _ := json.Marshal(plainCodes)
		encoded := base64.StdEncoding.EncodeToString(codesJSON)
		http.SetCookie(w, &http.Cookie{
			Name:     "mfa_backup_show",
			Value:    encoded,
			Path:     "/me/mfa/backup-codes",
			HttpOnly: true,
			MaxAge:   300, // 5 minutos para ver/descargar
			SameSite: http.SameSiteLaxMode,
		})

		http.Redirect(w, r, "/me/mfa/backup-codes", http.StatusSeeOther)
	}
}

// ── GET /me/mfa/backup-codes ──────────────────────────────────

func (h *MeHandler) MFABackupCodes(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Leer códigos desde cookie temporal
		cookie, err := r.Cookie("mfa_backup_show")

		type BackupData struct {
			Codes     []string
			FromSetup bool // true = recién configurado, false = regenerados
		}

		var codes []string
		fromSetup := false

		if err == nil && cookie.Value != "" {
			raw, decErr := base64.StdEncoding.DecodeString(cookie.Value)
			if decErr == nil {
				json.Unmarshal(raw, &codes)
				fromSetup = true
			}
			// Limpiar cookie
			http.SetCookie(w, &http.Cookie{
				Name: "mfa_backup_show", Value: "", Path: "/me/mfa/backup-codes", MaxAge: -1,
			})
		}

		// Si no hay cookie (acceso directo), no mostramos los códigos en texto plano
		// por seguridad — solo permitimos descargar si vienen del setup

		data := h.meData(r, "mfa", navUser)
		data.Data = BackupData{Codes: codes, FromSetup: fromSetup}
		h.render(w, "mfa-backup-codes.html", data)
	}
}

// ── GET /me/mfa/backup-codes/download ────────────────────────

func (h *MeHandler) MFABackupCodesDownload(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID := mustParseUUID(navUser.ID)

		codes, err := authstore.GetUnusedBackupCodes(ctx, h.pool, userID)
		if err != nil || len(codes) == 0 {
			http.Redirect(w, r, "/me/mfa", http.StatusSeeOther)
			return
		}

		var buf bytes.Buffer
		buf.WriteString("switchboard — MFA Backup Codes\n")
		buf.WriteString("================================\n")
		buf.WriteString("Keep these codes in a safe place.\n")
		buf.WriteString("Each code can only be used once.\n\n")

		for i, c := range codes {
			plain, err := h.enc.Decrypt(c.CodeEncrypted)
			if err != nil {
				continue
			}
			buf.WriteString(fmt.Sprintf("%2d. %s\n", i+1, plain))
		}

		buf.WriteString("\nGenerated: " + time.Now().UTC().Format("2006-01-02 15:04:05 UTC") + "\n")
		buf.WriteString("Account:   " + navUser.Email + "\n")

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="switchboard-backup-codes.txt"`)
		w.Write(buf.Bytes())
	}
}

// ── POST /me/mfa/disable ──────────────────────────────────────

func (h *MeHandler) MFADisable(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID := mustParseUUID(navUser.ID)
		code := strings.TrimSpace(r.FormValue("code"))

		user, err := authstore.GetUserByID(ctx, h.pool, userID)
		if err != nil || !user.MFAEnabled || user.MFASecret == nil {
			http.Redirect(w, r, "/me/mfa", http.StatusSeeOther)
			return
		}

		secret, err := h.enc.Decrypt(*user.MFASecret)
		if err != nil || !auth.ValidateTOTP(code, secret) {
			data := h.meData(r, "mfa", navUser)
			data.Flash = "invalid_code"
			data.FlashType = "error"
			h.render(w, "mfa.html", data)
			return
		}

		authstore.DisableMFA(ctx, h.pool, userID)
		authstore.DeleteBackupCodes(ctx, h.pool, userID)

		ip := auth.ClientIP(r)
		ua := auth.UserAgent(r)
		go authstore.InsertAuditLog(context.Background(), h.pool, &userID, "mfa_disabled", ip, ua, nil)

		http.Redirect(w, r, "/me/mfa?flash=mfa_disabled&type=success", http.StatusSeeOther)
	}
}

// ── generateBackupCodes ───────────────────────────────────────

func (h *MeHandler) generateBackupCodes(ctx context.Context, userID uuid.UUID) ([]authstore.BackupCode, []string, error) {
	// Borrar codes anteriores
	authstore.DeleteBackupCodes(ctx, h.pool, userID)

	var dbCodes []authstore.BackupCode
	var plainCodes []string

	for i := 0; i < 10; i++ {
		plain, err := auth.GenerateRandomHex(5) // 10 chars hex
		if err != nil {
			return nil, nil, err
		}
		// Formatear como XXXXX-XXXXX
		formatted := plain[:5] + "-" + plain[5:]

		hash, err := auth.HashPassword(formatted)
		if err != nil {
			return nil, nil, err
		}
		encrypted, err := h.enc.Encrypt(formatted)
		if err != nil {
			return nil, nil, err
		}

		dbCodes = append(dbCodes, authstore.BackupCode{
			UserID:        userID,
			CodeHash:      hash,
			CodeEncrypted: encrypted,
		})
		plainCodes = append(plainCodes, formatted)
	}

	if err := authstore.CreateBackupCodes(ctx, h.pool, dbCodes); err != nil {
		return nil, nil, err
	}

	return dbCodes, plainCodes, nil
}

// ── Render helper ─────────────────────────────────────────────

func (h *MeHandler) render(w http.ResponseWriter, page string, data *MePageData) {
	tmpl, err := template.ParseFiles(
		"services/gateway/templates/base.html",
		"services/gateway/templates/partials/navbar_user.html",
		"services/gateway/templates/me/layout.html",
		"services/gateway/templates/me/"+page,
	)
	if err != nil {
		h.log.Error().Err(err).Str("page", page).Msg("me: parse template")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.ExecuteTemplate(w, "base.html", data); err != nil {
		h.log.Error().Err(err).Msg("me: execute template")
	}
}

func mustParseUUID(s string) uuid.UUID {
	id, _ := uuid.Parse(s)
	return id
}

func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}
