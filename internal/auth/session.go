package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	cookieName = "sb_session"
	cookieTTL  = 30 * 24 * time.Hour
)

// SessionManager firma y verifica cookies de sesión con HMAC-SHA256.
type SessionManager struct {
	secret []byte
}

func NewSessionManager(hexSecret string) (*SessionManager, error) {
	secret, err := hex.DecodeString(hexSecret)
	if err != nil {
		return nil, errors.New("session secret must be a valid hex string")
	}
	if len(secret) < 32 {
		return nil, errors.New("session secret must be at least 32 bytes (64 hex chars)")
	}
	return &SessionManager{secret: secret}, nil
}

// SessionData es lo que se almacena en el contexto después de validar la cookie.
type SessionData struct {
	SessionID   string
	UserID      uuid.UUID
	Email       string
	Role        string
	MFAVerified bool
}

// SetCookie firma el sessionID y lo escribe como cookie httpOnly.
func (sm *SessionManager) SetCookie(w http.ResponseWriter, sessionID string) {
	signed := sm.sign(sessionID)
	value := sessionID + "." + signed

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // true en producción con TLS
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(cookieTTL.Seconds()),
	})
}

// ClearCookie invalida la cookie en el browser.
func (sm *SessionManager) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// ParseCookie extrae y verifica el sessionID de la cookie.
// Devuelve el sessionID si la firma es válida.
func (sm *SessionManager) ParseCookie(r *http.Request) (string, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return "", errors.New("no session cookie")
	}

	parts := strings.SplitN(cookie.Value, ".", 2)
	if len(parts) != 2 {
		return "", errors.New("malformed session cookie")
	}

	sessionID, sig := parts[0], parts[1]

	expected := sm.sign(sessionID)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return "", errors.New("invalid session signature")
	}

	return sessionID, nil
}

func (sm *SessionManager) sign(value string) string {
	mac := hmac.New(sha256.New, sm.secret)
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

// ── Context helpers ───────────────────────────────────────────

type contextKey string

const sessionContextKey contextKey = "session"

// WithSession inyecta la sesión en el contexto.
func WithSession(ctx context.Context, s *SessionData) context.Context {
	return context.WithValue(ctx, sessionContextKey, s)
}

// SessionFromContext extrae la sesión del contexto.
// Devuelve nil si no hay sesión.
func SessionFromContext(ctx context.Context) *SessionData {
	s, _ := ctx.Value(sessionContextKey).(*SessionData)
	return s
}
