package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"golang.org/x/oauth2"

	"switchboard/internal/auth"
	"switchboard/internal/config"
	authstore "switchboard/internal/store/auth"
)

type OAuthHandler struct {
	pool     *pgxpool.Pool
	log      zerolog.Logger
	cfg      *config.Config
	sm       *auth.SessionManager
	oauthCfg *oauth2.Config
}

func NewOAuthHandler(
	pool *pgxpool.Pool,
	log zerolog.Logger,
	cfg *config.Config,
	sm *auth.SessionManager,
) *OAuthHandler {
	return &OAuthHandler{
		pool: pool,
		log:  log,
		cfg:  cfg,
		sm:   sm,
		oauthCfg: auth.NewGoogleOAuthConfig(
			cfg.Google.ClientID,
			cfg.Google.ClientSecret,
			cfg.Google.RedirectURI,
		),
	}
}

// ── GET /auth/google ──────────────────────────────────────────
// Genera state CSRF, guarda en cookie y redirige a Google.

func (h *OAuthHandler) RedirectToGoogle(w http.ResponseWriter, r *http.Request) {
	state, err := generateState()
	if err != nil {
		h.log.Error().Err(err).Msg("oauth: generate state")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Guardar state en cookie httpOnly (5 min)
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // true en producción
		SameSite: http.SameSiteLaxMode,
		MaxAge:   300,
	})

	url := h.oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// ── GET /auth/google/callback ─────────────────────────────────

func (h *OAuthHandler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ip := auth.ClientIP(r)
	ua := auth.UserAgent(r)

	// ── 1. Verificar state CSRF ───────────────────────────────
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		h.log.Warn().Str("ip", ip).Msg("oauth: state mismatch")
		http.Redirect(w, r, "/auth/signin?error=oauth_state", http.StatusSeeOther)
		return
	}
	// Limpiar cookie de state
	http.SetCookie(w, &http.Cookie{
		Name: "oauth_state", Value: "", Path: "/", MaxAge: -1,
	})

	// ── 2. Intercambiar code por token ────────────────────────
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "/auth/signin?error=oauth_code", http.StatusSeeOther)
		return
	}

	token, err := h.oauthCfg.Exchange(ctx, code)
	if err != nil {
		h.log.Error().Err(err).Msg("oauth: exchange")
		http.Redirect(w, r, "/auth/signin?error=oauth_exchange", http.StatusSeeOther)
		return
	}

	// ── 3. Obtener perfil de Google ───────────────────────────
	profile, err := fetchGoogleProfile(ctx, h.oauthCfg, token)
	if err != nil {
		h.log.Error().Err(err).Msg("oauth: fetch profile")
		http.Redirect(w, r, "/auth/signin?error=oauth_profile", http.StatusSeeOther)
		return
	}

	if profile.Email == "" || !profile.VerifiedEmail {
		h.log.Warn().Str("sub", profile.Sub).Msg("oauth: unverified email")
		http.Redirect(w, r, "/auth/signin?error=oauth_unverified", http.StatusSeeOther)
		return
	}

	// ── 4. Buscar o crear usuario ─────────────────────────────
	userID, err := h.upsertOAuthUser(ctx, profile, token)
	if err != nil {
		h.log.Error().Err(err).Msg("oauth: upsert user")
		http.Redirect(w, r, "/auth/signin?error=oauth_user", http.StatusSeeOther)
		return
	}

	// ── 5. Crear sesión ───────────────────────────────────────
	sessionID, err := auth.GenerateRandomHex(32)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := authstore.CreateSession(ctx, h.pool, authstore.Session{
		ID:          sessionID,
		UserID:      userID,
		IPAddress:   &ip,
		UserAgent:   &ua,
		MFAVerified: true, // Google ya verificó identidad
		ExpiresAt:   time.Now().Add(h.cfg.Auth.SessionDuration),
	}); err != nil {
		h.log.Error().Err(err).Msg("oauth: create session")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	authstore.UpdateLastLogin(ctx, h.pool, userID)
	h.auditOAuth(ctx, userID, "login", ip, ua, profile.Email)

	h.sm.SetCookie(w, sessionID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ── Helpers ───────────────────────────────────────────────────

type googleProfile struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"email_verified"`
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	Picture       string `json:"picture"`
}

func fetchGoogleProfile(ctx context.Context, cfg *oauth2.Config, token *oauth2.Token) (*googleProfile, error) {
	client := cfg.Client(ctx, token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v3/userinfo")
	if err != nil {
		return nil, fmt.Errorf("userinfo request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var p googleProfile
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("unmarshal profile: %w", err)
	}
	return &p, nil
}

func (h *OAuthHandler) upsertOAuthUser(
	ctx context.Context,
	profile *googleProfile,
	token *oauth2.Token,
) (uuid.UUID, error) {

	// ¿Ya existe el provider?
	existing, err := authstore.GetOAuthProvider(ctx, h.pool, "google", profile.Sub)
	if err == nil {
		// Ya existe — actualizar tokens y devolver userID
		rawProfile, _ := json.Marshal(profile)
		accessToken := token.AccessToken
		refreshToken := token.RefreshToken
		var expiresAt *time.Time
		if !token.Expiry.IsZero() {
			t := token.Expiry
			expiresAt = &t
		}
		authstore.UpsertOAuthProvider(ctx, h.pool, existing.UserID, "google", profile.Sub,
			&accessToken, ptrStr(refreshToken), expiresAt, rawProfile)
		// Actualizar avatar si cambió
		if profile.Picture != "" {
			authstore.UpdateAvatar(ctx, h.pool, existing.UserID, profile.Picture)
		}
		return existing.UserID, nil
	}

	// ¿Existe usuario con ese email?
	user, err := authstore.GetUserByEmail(ctx, h.pool, profile.Email)
	if err != nil {
		// Crear usuario nuevo
		displayName := strings.TrimSpace(profile.Name)
		userID, err := authstore.CreateUser(ctx, h.pool, profile.Email, "", displayName)
		if err != nil {
			return uuid.Nil, fmt.Errorf("create user: %w", err)
		}
		// Marcar email verificado (Google lo verificó)
		authstore.VerifyUserEmail(ctx, h.pool, userID)
		if profile.Picture != "" {
			authstore.UpdateAvatar(ctx, h.pool, userID, profile.Picture)
		}
		user = &authstore.User{ID: userID, Email: profile.Email}
	}

	// Vincular OAuth provider
	rawProfile, _ := json.Marshal(profile)
	accessToken := token.AccessToken
	refreshToken := token.RefreshToken
	var expiresAt *time.Time
	if !token.Expiry.IsZero() {
		t := token.Expiry
		expiresAt = &t
	}
	if err := authstore.UpsertOAuthProvider(ctx, h.pool, user.ID, "google", profile.Sub,
		&accessToken, ptrStr(refreshToken), expiresAt, rawProfile,
	); err != nil {
		return uuid.Nil, fmt.Errorf("upsert provider: %w", err)
	}

	return user.ID, nil
}

func (h *OAuthHandler) auditOAuth(ctx context.Context, userID uuid.UUID, action, ip, ua, email string) {
	go func() {
		ctx2, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		authstore.InsertAuditLog(ctx2, h.pool, &userID, "oauth_link", ip, ua, map[string]any{
			"provider": "google",
			"email":    email,
			"action":   action,
		})
	}()
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func ptrStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
