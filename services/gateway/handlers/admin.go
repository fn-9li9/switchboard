package handlers

import (
	"context"
	"net/http"
	"strings"

	"html/template"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"switchboard/internal/auth"
	"switchboard/internal/config"
	authstore "switchboard/internal/store/auth"
)

type AdminHandler struct {
	pool *pgxpool.Pool
	log  zerolog.Logger
	cfg  *config.Config
	sm   *auth.SessionManager
}

func NewAdminHandler(pool *pgxpool.Pool, log zerolog.Logger, cfg *config.Config, sm *auth.SessionManager) *AdminHandler {
	return &AdminHandler{pool: pool, log: log, cfg: cfg, sm: sm}
}

type AdminPageData struct {
	User      *NavUser
	ActiveTab string
	Flash     string
	FlashType string
	Data      any
}

func (h *AdminHandler) render(w http.ResponseWriter, navUser *NavUser, page string, data *AdminPageData) {
	funcMap := template.FuncMap{
		"actionColor": func(action string) string {
			switch {
			case strings.Contains(action, "delete") || strings.Contains(action, "lock"):
				return "bg-ctp-red/10 text-ctp-red"
			case strings.Contains(action, "login") || strings.Contains(action, "oauth"):
				return "bg-ctp-green/10 text-ctp-green"
			case strings.Contains(action, "mfa"):
				return "bg-ctp-mauve/10 text-ctp-mauve"
			case strings.Contains(action, "password"):
				return "bg-ctp-blue/10 text-ctp-blue"
			case strings.Contains(action, "role") || strings.Contains(action, "session"):
				return "bg-ctp-peach/10 text-ctp-peach"
			default:
				return "bg-ctp-surface1 text-ctp-overlay0"
			}
		},
	}

	tmpl, err := template.New("").Funcs(funcMap).ParseFiles(
		"services/gateway/templates/base.html",
		"services/gateway/templates/partials/navbar_user.html",
		"services/gateway/templates/me/layout.html",
		"services/gateway/templates/admin/"+page,
	)
	if err != nil {
		h.log.Error().Err(err).Str("page", page).Msg("admin: parse template")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.ExecuteTemplate(w, "base.html", data); err != nil {
		h.log.Error().Err(err).Msg("admin: execute template")
	}
}

func requireRoot(navUser *NavUser, w http.ResponseWriter, r *http.Request) bool {
	if navUser == nil || navUser.Role != "root" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

// ── GET /me/users ─────────────────────────────────────────────

func (h *AdminHandler) Users(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireRoot(navUser, w, r) {
			return
		}
		ctx := r.Context()

		search := strings.TrimSpace(r.URL.Query().Get("q"))
		users, err := authstore.ListUsers(ctx, h.pool, search)
		if err != nil {
			h.log.Error().Err(err).Msg("admin/users: list")
			users = nil
		}

		type UsersData struct {
			Users  []authstore.UserRow
			Search string
		}

		h.render(w, navUser, "users.html", &AdminPageData{
			User:      navUser,
			ActiveTab: "admin-users", // ← diferencia settings de users
			Flash:     r.URL.Query().Get("flash"),
			FlashType: r.URL.Query().Get("type"),
			Data:      UsersData{Users: users, Search: search},
		})
	}
}

// ── POST /me/users/:id/role ───────────────────────────────────

func (h *AdminHandler) UpdateRole(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireRoot(navUser, w, r) {
			return
		}
		ctx := r.Context()

		targetID := mustParseUUID(r.PathValue("id"))
		role := r.FormValue("role")

		if role != "default" && role != "root" {
			http.Redirect(w, r, "/me/users?flash=invalid_role&type=error", http.StatusSeeOther)
			return
		}
		// No puede cambiar su propio rol
		if targetID == mustParseUUID(navUser.ID) {
			http.Redirect(w, r, "/me/users?flash=self_role&type=error", http.StatusSeeOther)
			return
		}

		authstore.UpdateUserRole(ctx, h.pool, targetID, role)

		ip := auth.ClientIP(r)
		ua := auth.UserAgent(r)
		rootID := mustParseUUID(navUser.ID)
		go authstore.InsertAuditLog(context.Background(), h.pool, &rootID, "user_role_change", ip, ua,
			map[string]any{"target": targetID.String(), "role": role})

		http.Redirect(w, r, "/me/users?flash=role_updated&type=success", http.StatusSeeOther)
	}
}

// ── POST /me/users/:id/activate ───────────────────────────────

func (h *AdminHandler) SetActive(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireRoot(navUser, w, r) {
			return
		}
		ctx := r.Context()

		targetID := mustParseUUID(r.PathValue("id"))
		active := r.FormValue("active") == "true"

		if targetID == mustParseUUID(navUser.ID) {
			http.Redirect(w, r, "/me/users?flash=self_deactivate&type=error", http.StatusSeeOther)
			return
		}

		authstore.SetUserActive(ctx, h.pool, targetID, active)

		if !active {
			authstore.RevokeAllUserSessions(ctx, h.pool, targetID, "")
		}

		ip := auth.ClientIP(r)
		ua := auth.UserAgent(r)
		rootID := mustParseUUID(navUser.ID)
		action := "user_activated"
		if !active {
			action = "user_deactivated"
		}
		go authstore.InsertAuditLog(context.Background(), h.pool, &rootID, action, ip, ua,
			map[string]any{"target": targetID.String()})

		flash := "user_activated"
		if !active {
			flash = "user_deactivated"
		}
		http.Redirect(w, r, "/me/users?flash="+flash+"&type=success", http.StatusSeeOther)
	}
}

// ── POST /me/users/:id/delete ─────────────────────────────────

func (h *AdminHandler) DeleteUser(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireRoot(navUser, w, r) {
			return
		}
		ctx := r.Context()

		targetID := mustParseUUID(r.PathValue("id"))

		if targetID == mustParseUUID(navUser.ID) {
			http.Redirect(w, r, "/me/users?flash=self_delete&type=error", http.StatusSeeOther)
			return
		}

		authstore.RevokeAllUserSessions(ctx, h.pool, targetID, "")
		authstore.DeleteUser(ctx, h.pool, targetID)

		ip := auth.ClientIP(r)
		ua := auth.UserAgent(r)
		rootID := mustParseUUID(navUser.ID)
		go authstore.InsertAuditLog(context.Background(), h.pool, &rootID, "user_deleted", ip, ua,
			map[string]any{"target": targetID.String()})

		http.Redirect(w, r, "/me/users?flash=user_deleted&type=success", http.StatusSeeOther)
	}
}

// ── POST /me/users/:id/revoke-sessions ────────────────────────

func (h *AdminHandler) RevokeSessions(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireRoot(navUser, w, r) {
			return
		}
		ctx := r.Context()

		targetID := mustParseUUID(r.PathValue("id"))
		authstore.RevokeAllUserSessions(ctx, h.pool, targetID, "")

		ip := auth.ClientIP(r)
		ua := auth.UserAgent(r)
		rootID := mustParseUUID(navUser.ID)
		go authstore.InsertAuditLog(context.Background(), h.pool, &rootID, "user_sessions_revoked", ip, ua,
			map[string]any{"target": targetID.String()})

		http.Redirect(w, r, "/me/users?flash=sessions_revoked&type=success", http.StatusSeeOther)
	}
}

// ── POST /me/users/:id/disable-mfa ───────────────────────────

func (h *AdminHandler) DisableMFA(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireRoot(navUser, w, r) {
			return
		}
		ctx := r.Context()

		targetID := mustParseUUID(r.PathValue("id"))
		authstore.DisableMFA(ctx, h.pool, targetID)
		authstore.DeleteBackupCodes(ctx, h.pool, targetID)

		ip := auth.ClientIP(r)
		ua := auth.UserAgent(r)
		rootID := mustParseUUID(navUser.ID)
		go authstore.InsertAuditLog(context.Background(), h.pool, &rootID, "user_mfa_disabled", ip, ua,
			map[string]any{"target": targetID.String()})

		http.Redirect(w, r, "/me/users?flash=mfa_disabled&type=success", http.StatusSeeOther)
	}
}

// ── GET /me/settings ─────────────────────────────────────────

func (h *AdminHandler) Settings(navUser *NavUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireRoot(navUser, w, r) {
			return
		}
		ctx := r.Context()

		stats, err := authstore.GetStats(ctx, h.pool)
		if err != nil {
			h.log.Error().Err(err).Msg("admin/settings: stats")
		}

		auditLog, err := authstore.ListAuditLog(ctx, h.pool, 50)
		if err != nil {
			h.log.Error().Err(err).Msg("admin/settings: audit log")
		}

		type SettingsData struct {
			Stats    *authstore.StatsRow
			AuditLog []authstore.AuditRow
		}

		h.render(w, navUser, "settings.html", &AdminPageData{
			User:      navUser,
			ActiveTab: "admin-settings",
			Data:      SettingsData{Stats: stats, AuditLog: auditLog},
		})
	}
}
