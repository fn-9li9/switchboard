package auth

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// Middleware agrupa las dependencias necesarias para los middlewares de auth.
type Middleware struct {
	sm   *SessionManager
	pool *pgxpool.Pool
	log  zerolog.Logger
}

func NewMiddleware(sm *SessionManager, pool *pgxpool.Pool, log zerolog.Logger) *Middleware {
	return &Middleware{sm: sm, pool: pool, log: log}
}

// RequireAuth valida la cookie, carga la sesión de Postgres e inyecta
// el SessionData en el contexto. Si falla, redirige a /login.
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := m.loadSession(r)
		if err != nil {
			m.log.Debug().Err(err).Str("path", r.URL.Path).Msg("auth: unauthorized")
			http.Redirect(w, r, "/auth/signin?next="+r.URL.Path, http.StatusSeeOther)
			return
		}
		ctx := WithSession(r.Context(), session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuth para handler func directamente
func (m *Middleware) RequireAuthFunc(next http.HandlerFunc) http.HandlerFunc {
	return m.RequireAuth(next).ServeHTTP
}

// RedirectIfAuth redirige al home si ya hay sesión activa.
// Útil para /auth/signin y /auth/signup.
func (m *Middleware) RedirectIfAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := m.loadSession(r)
		if err == nil && session != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loadSession lee la cookie, verifica la firma HMAC, consulta Postgres
// y devuelve un SessionData si todo es válido.
func (m *Middleware) loadSession(r *http.Request) (*SessionData, error) {
	sessionID, err := m.sm.ParseCookie(r)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	var (
		userID      uuid.UUID
		email       string
		role        string
		mfaVerified bool
		expiresAt   time.Time
		isActive    bool
	)

	err = m.pool.QueryRow(ctx, `
		SELECT s.user_id, u.email, u.role, s.mfa_verified, s.expires_at, s.is_active
		FROM auth.sessions s
		JOIN auth.users u ON u.id = s.user_id
		WHERE s.id = $1
		  AND u.is_active = true
	`, sessionID).Scan(&userID, &email, &role, &mfaVerified, &expiresAt, &isActive)
	if err != nil {
		return nil, err
	}

	if !isActive || time.Now().After(expiresAt) {
		return nil, ErrSessionExpired
	}

	// Actualizar last_seen_at sin bloquear
	go func() {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel2()
		m.pool.Exec(ctx2,
			`UPDATE auth.sessions SET last_seen_at = NOW() WHERE id = $1`,
			sessionID,
		)
	}()

	return &SessionData{
		SessionID:   sessionID,
		UserID:      userID,
		Email:       email,
		Role:        role,
		MFAVerified: mfaVerified,
	}, nil
}

// ── Errores exportados ────────────────────────────────────────

var (
	ErrSessionExpired  = authError("session expired")
	ErrSessionInactive = authError("session inactive")
	ErrUnauthorized    = authError("unauthorized")
)

type authError string

func (e authError) Error() string { return string(e) }
