package authstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")
var ErrDuplicate = errors.New("already exists")

// ── User ──────────────────────────────────────────────────────

type User struct {
	ID            uuid.UUID
	Email         string
	EmailVerified bool
	PasswordHash  *string
	AvatarURL     *string
	DisplayName   *string
	Role          string
	IsActive      bool
	MFAEnabled    bool
	MFASecret     *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastLoginAt   *time.Time
}

func CreateUser(ctx context.Context, pool *pgxpool.Pool, email, passwordHash, displayName string) (uuid.UUID, error) {
	var id uuid.UUID
	var hashArg interface{}
	if passwordHash == "" {
		hashArg = nil // NULL para usuarios OAuth
	} else {
		hashArg = passwordHash
	}
	err := pool.QueryRow(ctx, `
		INSERT INTO auth.users (email, password_hash, display_name)
		VALUES ($1, $2, $3)
		RETURNING id
	`, email, hashArg, displayName).Scan(&id)
	if err != nil {
		if isDuplicateErr(err) {
			return uuid.Nil, ErrDuplicate
		}
		return uuid.Nil, err
	}
	return id, nil
}

func GetUserByEmail(ctx context.Context, pool *pgxpool.Pool, email string) (*User, error) {
	u := &User{}
	err := pool.QueryRow(ctx, `
		SELECT id, email, email_verified, password_hash, avatar_url, display_name,
		       role, is_active, mfa_enabled, mfa_secret, created_at, updated_at, last_login_at
		FROM auth.users
		WHERE email = $1
	`, email).Scan(
		&u.ID, &u.Email, &u.EmailVerified, &u.PasswordHash,
		&u.AvatarURL, &u.DisplayName, &u.Role, &u.IsActive,
		&u.MFAEnabled, &u.MFASecret,
		&u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return u, nil
}

func GetUserByID(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*User, error) {
	u := &User{}
	err := pool.QueryRow(ctx, `
		SELECT id, email, email_verified, password_hash, avatar_url, display_name,
		       role, is_active, mfa_enabled, mfa_secret, created_at, updated_at, last_login_at
		FROM auth.users
		WHERE id = $1
	`, id).Scan(
		&u.ID, &u.Email, &u.EmailVerified, &u.PasswordHash,
		&u.AvatarURL, &u.DisplayName, &u.Role, &u.IsActive,
		&u.MFAEnabled, &u.MFASecret,
		&u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return u, nil
}

func VerifyUserEmail(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) error {
	_, err := pool.Exec(ctx, `
		UPDATE auth.users
		SET email_verified = true, updated_at = NOW()
		WHERE id = $1
	`, userID)
	return err
}

func UpdateLastLogin(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) error {
	_, err := pool.Exec(ctx, `
		UPDATE auth.users
		SET last_login_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, userID)
	return err
}

func UpdatePassword(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, newHash string) error {
	_, err := pool.Exec(ctx, `
		UPDATE auth.users
		SET password_hash = $2, updated_at = NOW()
		WHERE id = $1
	`, userID, newHash)
	return err
}

func UpdateMFASecret(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, encryptedSecret string) error {
	_, err := pool.Exec(ctx, `
		UPDATE auth.users
		SET mfa_secret = $2, updated_at = NOW()
		WHERE id = $1
	`, userID, encryptedSecret)
	return err
}

func EnableMFA(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) error {
	_, err := pool.Exec(ctx, `
		UPDATE auth.users
		SET mfa_enabled = true, updated_at = NOW()
		WHERE id = $1
	`, userID)
	return err
}

func DisableMFA(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) error {
	_, err := pool.Exec(ctx, `
		UPDATE auth.users
		SET mfa_enabled = false, mfa_secret = NULL, updated_at = NOW()
		WHERE id = $1
	`, userID)
	return err
}

func UpdateAvatar(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, avatarURL string) error {
	_, err := pool.Exec(ctx, `
		UPDATE auth.users
		SET avatar_url = $2, updated_at = NOW()
		WHERE id = $1
	`, userID, avatarURL)
	return err
}

// ── Sessions ──────────────────────────────────────────────────

type Session struct {
	ID          string
	UserID      uuid.UUID
	IPAddress   *string // lo guardamos como string al insertar
	UserAgent   *string
	Country     *string
	IsActive    bool
	MFAVerified bool
	ExpiresAt   time.Time
	CreatedAt   time.Time
	LastSeenAt  time.Time
}

func CreateSession(ctx context.Context, pool *pgxpool.Pool, s Session) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO auth.sessions
			(id, user_id, ip_address, user_agent, country, mfa_verified, expires_at)
		VALUES ($1, $2, $3::inet, $4, $5, $6, $7)
	`, s.ID, s.UserID, s.IPAddress, s.UserAgent, s.Country, s.MFAVerified, s.ExpiresAt)
	return err
}

func GetSession(ctx context.Context, pool *pgxpool.Pool, sessionID string) (*Session, error) {
	s := &Session{}

	var ipRaw pgtype.Text
	var uaRaw pgtype.Text
	var countryRaw pgtype.Text

	err := pool.QueryRow(ctx, `
		SELECT id, user_id,
		       CAST(ip_address AS TEXT),
		       user_agent,
		       country,
		       is_active, mfa_verified, expires_at, created_at, last_seen_at
		FROM auth.sessions
		WHERE id = $1
	`, sessionID).Scan(
		&s.ID, &s.UserID,
		&ipRaw, &uaRaw, &countryRaw,
		&s.IsActive, &s.MFAVerified, &s.ExpiresAt, &s.CreatedAt, &s.LastSeenAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if ipRaw.Valid {
		s.IPAddress = &ipRaw.String
	}
	if uaRaw.Valid {
		s.UserAgent = &uaRaw.String
	}
	if countryRaw.Valid {
		s.Country = &countryRaw.String
	}

	return s, nil
}

func ListUserSessions(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) ([]Session, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, user_id,
		       CAST(ip_address AS TEXT),
		       user_agent,
		       country,
		       is_active, mfa_verified, expires_at, created_at, last_seen_at
		FROM auth.sessions
		WHERE user_id = $1
		  AND is_active = true
		  AND expires_at > NOW()
		ORDER BY last_seen_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		var ipRaw, uaRaw, countryRaw pgtype.Text
		if err := rows.Scan(
			&s.ID, &s.UserID,
			&ipRaw, &uaRaw, &countryRaw,
			&s.IsActive, &s.MFAVerified, &s.ExpiresAt, &s.CreatedAt, &s.LastSeenAt,
		); err != nil {
			return nil, err
		}
		if ipRaw.Valid {
			s.IPAddress = &ipRaw.String
		}
		if uaRaw.Valid {
			s.UserAgent = &uaRaw.String
		}
		if countryRaw.Valid {
			s.Country = &countryRaw.String
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func RevokeSession(ctx context.Context, pool *pgxpool.Pool, sessionID string) error {
	_, err := pool.Exec(ctx, `
		UPDATE auth.sessions
		SET is_active = false
		WHERE id = $1
	`, sessionID)
	return err
}

func RevokeAllUserSessions(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, exceptID string) error {
	_, err := pool.Exec(ctx, `
		UPDATE auth.sessions
		SET is_active = false
		WHERE user_id = $1 AND id != $2
	`, userID, exceptID)
	return err
}

func SetMFAVerified(ctx context.Context, pool *pgxpool.Pool, sessionID string) error {
	_, err := pool.Exec(ctx, `
		UPDATE auth.sessions
		SET mfa_verified = true
		WHERE id = $1
	`, sessionID)
	return err
}

// ── Verification tokens ───────────────────────────────────────

type VerificationToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Token     string
	TokenType string
	NewEmail  *string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

func CreateVerificationToken(
	ctx context.Context,
	pool *pgxpool.Pool,
	userID uuid.UUID,
	token, tokenType string,
	expiresAt time.Time,
) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO auth.verification_tokens (user_id, token, token_type, expires_at)
		VALUES ($1, $2, $3, $4)
	`, userID, token, tokenType, expiresAt)
	return err
}

func GetVerificationToken(ctx context.Context, pool *pgxpool.Pool, token string) (*VerificationToken, error) {
	vt := &VerificationToken{}
	err := pool.QueryRow(ctx, `
		SELECT id, user_id, token, token_type, new_email, expires_at, used_at, created_at
		FROM auth.verification_tokens
		WHERE token = $1
	`, token).Scan(
		&vt.ID, &vt.UserID, &vt.Token, &vt.TokenType,
		&vt.NewEmail, &vt.ExpiresAt, &vt.UsedAt, &vt.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return vt, nil
}

func UseVerificationToken(ctx context.Context, pool *pgxpool.Pool, token string) error {
	result, err := pool.Exec(ctx, `
		UPDATE auth.verification_tokens
		SET used_at = NOW()
		WHERE token = $1
		  AND used_at IS NULL
		  AND expires_at > NOW()
	`, token)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("token invalid, expired or already used")
	}
	return nil
}

// ── Login attempts ────────────────────────────────────────────

func RecordLoginAttempt(
	ctx context.Context,
	pool *pgxpool.Pool,
	email, ipAddress, userAgent string,
	success bool,
	failureReason *string,
) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO auth.login_attempts (email, ip_address, user_agent, success, failure_reason)
		VALUES ($1, $2::inet, $3, $4, $5)
	`, email, ipAddress, userAgent, success, failureReason)
	return err
}

// CountRecentFailures cuenta intentos fallidos en la última ventana de tiempo.
// Útil para lockout por email o por IP.
func CountRecentFailures(
	ctx context.Context,
	pool *pgxpool.Pool,
	email, ipAddress string,
	window time.Duration,
) (byEmail, byIP int, err error) {
	since := time.Now().Add(-window)

	err = pool.QueryRow(ctx, `
		SELECT
		    COUNT(*) FILTER (WHERE email = $1 AND success = false),
		    COUNT(*) FILTER (WHERE ip_address = $2::inet AND success = false)
		FROM auth.login_attempts
		WHERE created_at > $3
		  AND success = false
	`, email, ipAddress, since).Scan(&byEmail, &byIP)
	return
}

// ── OAuth providers ───────────────────────────────────────────

type OAuthProvider struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	Provider       string
	ProviderUID    string
	AccessToken    *string
	RefreshToken   *string
	TokenExpiresAt *time.Time
	RawProfile     []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func UpsertOAuthProvider(
	ctx context.Context,
	pool *pgxpool.Pool,
	userID uuid.UUID,
	provider, providerUID string,
	accessToken, refreshToken *string,
	tokenExpiresAt *time.Time,
	rawProfile []byte,
) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO auth.oauth_providers
			(user_id, provider, provider_uid, access_token, refresh_token, token_expires_at, raw_profile)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (provider, provider_uid) DO UPDATE SET
			user_id          = EXCLUDED.user_id,
			access_token     = EXCLUDED.access_token,
			refresh_token    = EXCLUDED.refresh_token,
			token_expires_at = EXCLUDED.token_expires_at,
			raw_profile      = EXCLUDED.raw_profile,
			updated_at       = NOW()
	`, userID, provider, providerUID, accessToken, refreshToken, tokenExpiresAt, rawProfile)
	return err
}

func GetOAuthProvider(
	ctx context.Context,
	pool *pgxpool.Pool,
	provider, providerUID string,
) (*OAuthProvider, error) {
	op := &OAuthProvider{}
	err := pool.QueryRow(ctx, `
		SELECT id, user_id, provider, provider_uid, access_token, refresh_token,
		       token_expires_at, raw_profile, created_at, updated_at
		FROM auth.oauth_providers
		WHERE provider = $1 AND provider_uid = $2
	`, provider, providerUID).Scan(
		&op.ID, &op.UserID, &op.Provider, &op.ProviderUID,
		&op.AccessToken, &op.RefreshToken, &op.TokenExpiresAt,
		&op.RawProfile, &op.CreatedAt, &op.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return op, nil
}

// ── Audit log ─────────────────────────────────────────────────

func InsertAuditLog(
	ctx context.Context,
	pool *pgxpool.Pool,
	userID *uuid.UUID,
	action, ipAddress, userAgent string,
	metadata map[string]any,
) error {
	metaJSON, err := marshalMetadata(metadata)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO auth.audit_log (user_id, action, ip_address, user_agent, metadata)
		VALUES ($1, $2, $3::inet, $4, $5)
	`, userID, action, ipAddress, userAgent, metaJSON)
	return err
}

// ── Backup codes ──────────────────────────────────────────────

type BackupCode struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	CodeHash      string
	CodeEncrypted string
	UsedAt        *time.Time
	CreatedAt     time.Time
}

func CreateBackupCodes(ctx context.Context, pool *pgxpool.Pool, codes []BackupCode) error {
	batch := &pgx.Batch{}
	for _, c := range codes {
		batch.Queue(`
			INSERT INTO auth.two_factor_backup_codes (user_id, code_hash, code_encrypted)
			VALUES ($1, $2, $3)
		`, c.UserID, c.CodeHash, c.CodeEncrypted)
	}
	return pool.SendBatch(ctx, batch).Close()
}

func GetUnusedBackupCodes(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) ([]BackupCode, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, user_id, code_hash, code_encrypted, used_at, created_at
		FROM auth.two_factor_backup_codes
		WHERE user_id = $1 AND used_at IS NULL
		ORDER BY created_at ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var codes []BackupCode
	for rows.Next() {
		var c BackupCode
		if err := rows.Scan(&c.ID, &c.UserID, &c.CodeHash, &c.CodeEncrypted, &c.UsedAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}

func UseBackupCode(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	result, err := pool.Exec(ctx, `
		UPDATE auth.two_factor_backup_codes
		SET used_at = NOW()
		WHERE id = $1 AND used_at IS NULL
	`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("backup code already used")
	}
	return nil
}

func DeleteBackupCodes(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) error {
	_, err := pool.Exec(ctx, `
		DELETE FROM auth.two_factor_backup_codes WHERE user_id = $1
	`, userID)
	return err
}

// ── Helpers ───────────────────────────────────────────────────

func isDuplicateErr(err error) bool {
	return err != nil && (contains(err.Error(), "duplicate key") ||
		contains(err.Error(), "unique constraint"))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(s) > 0 && containsRune(s, substr))
}

func containsRune(s, substr string) bool {
	for i := range s {
		if i+len(substr) <= len(s) && s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func marshalMetadata(m map[string]any) ([]byte, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
