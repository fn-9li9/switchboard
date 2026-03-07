package handlers

import (
	"context"
	"fmt"
	"html/template"
	"net/http"

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
