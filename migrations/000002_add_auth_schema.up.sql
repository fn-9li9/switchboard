-- ── Extensión necesaria para gen_random_uuid() ────────────────
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ── Schema ────────────────────────────────────────────────────
CREATE SCHEMA IF NOT EXISTS auth;

-- ── users ─────────────────────────────────────────────────────
CREATE TABLE auth.users (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email          TEXT UNIQUE NOT NULL,
    email_verified BOOLEAN NOT NULL DEFAULT false,
    password_hash  TEXT,
    avatar_url     TEXT,
    display_name   TEXT,
    role           TEXT NOT NULL DEFAULT 'default'
                       CHECK (role IN ('default', 'root')),
    is_active      BOOLEAN NOT NULL DEFAULT true,
    mfa_enabled    BOOLEAN NOT NULL DEFAULT false,
    mfa_secret     TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at  TIMESTAMPTZ
);

CREATE INDEX idx_users_email ON auth.users (email);

-- ── oauth_providers ───────────────────────────────────────────
CREATE TABLE auth.oauth_providers (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL
                         REFERENCES auth.users(id) ON DELETE CASCADE,
    provider         TEXT NOT NULL
                         CHECK (provider IN ('google')),
    provider_uid     TEXT NOT NULL,
    access_token     TEXT,
    refresh_token    TEXT,
    token_expires_at TIMESTAMPTZ,
    raw_profile      JSONB NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, provider_uid)
);

CREATE INDEX idx_oauth_user_id      ON auth.oauth_providers (user_id);
CREATE INDEX idx_oauth_provider_uid ON auth.oauth_providers (provider, provider_uid);

-- ── sessions ──────────────────────────────────────────────────
CREATE TABLE auth.sessions (
    id           TEXT PRIMARY KEY,              -- crypto/rand hex 32, no UUID
    user_id      UUID NOT NULL
                     REFERENCES auth.users(id) ON DELETE CASCADE,
    ip_address   INET,
    user_agent   TEXT,
    country      TEXT,
    is_active    BOOLEAN NOT NULL DEFAULT true,
    mfa_verified BOOLEAN NOT NULL DEFAULT false,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sessions_user_id    ON auth.sessions (user_id);
CREATE INDEX idx_sessions_expires_at ON auth.sessions (expires_at);

-- ── verification_tokens ───────────────────────────────────────
CREATE TABLE auth.verification_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL
                   REFERENCES auth.users(id) ON DELETE CASCADE,
    token      TEXT UNIQUE NOT NULL,
    token_type TEXT NOT NULL
                   CHECK (token_type IN ('email_verify', 'password_reset', 'email_change')),
    new_email  TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_vtokens_token   ON auth.verification_tokens (token);
CREATE INDEX idx_vtokens_user_id ON auth.verification_tokens (user_id, token_type);

-- ── two_factor_backup_codes ───────────────────────────────────
CREATE TABLE auth.two_factor_backup_codes (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL
                       REFERENCES auth.users(id) ON DELETE CASCADE,
    code_hash      TEXT NOT NULL,
    code_encrypted TEXT NOT NULL,
    used_at        TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_backup_codes_user_id ON auth.two_factor_backup_codes (user_id);

-- ── login_attempts ────────────────────────────────────────────
CREATE TABLE auth.login_attempts (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email          TEXT NOT NULL,
    ip_address     INET NOT NULL,
    user_agent     TEXT,
    success        BOOLEAN NOT NULL,
    failure_reason TEXT
                       CHECK (failure_reason IN (
                           'bad_password', 'unverified', 'mfa_failed',
                           'turnstile', 'locked', 'oauth_error'
                       )),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_attempts_email      ON auth.login_attempts (email,      created_at DESC);
CREATE INDEX idx_attempts_ip         ON auth.login_attempts (ip_address, created_at DESC);
CREATE INDEX idx_attempts_created_at ON auth.login_attempts (created_at DESC);

-- ── audit_log ─────────────────────────────────────────────────
CREATE TABLE auth.audit_log (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID REFERENCES auth.users(id) ON DELETE SET NULL,
    action     TEXT NOT NULL
                   CHECK (action IN (
                       'register', 'login', 'logout',
                       'mfa_enable', 'mfa_disable', 'mfa_verified',
                       'password_change', 'password_reset_request', 'password_reset_done',
                       'oauth_link', 'oauth_unlink',
                       'email_verify', 'email_change',
                       'backup_code_view', 'backup_code_use', 'backup_codes_regen',
                       'session_revoke', 'account_lock', 'account_unlock'
                   )),
    ip_address INET,
    user_agent TEXT,
    metadata   JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_user_id    ON auth.audit_log (user_id,   created_at DESC);
CREATE INDEX idx_audit_action     ON auth.audit_log (action,    created_at DESC);
CREATE INDEX idx_audit_created_at ON auth.audit_log (created_at DESC);

-- ── Trigger auto updated_at ───────────────────────────────────
CREATE OR REPLACE FUNCTION auth.set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_users_updated_at
    BEFORE UPDATE ON auth.users
    FOR EACH ROW EXECUTE FUNCTION auth.set_updated_at();

CREATE TRIGGER trg_oauth_updated_at
    BEFORE UPDATE ON auth.oauth_providers
    FOR EACH ROW EXECUTE FUNCTION auth.set_updated_at();

-- ── Función cleanup ───────────────────────────────────────────
CREATE OR REPLACE FUNCTION auth.cleanup_expired() RETURNS void AS $$
BEGIN
    DELETE FROM auth.sessions
        WHERE expires_at < NOW() - INTERVAL '7 days';

    DELETE FROM auth.verification_tokens
        WHERE used_at IS NOT NULL
          AND used_at < NOW() - INTERVAL '24 hours';

    DELETE FROM auth.login_attempts
        WHERE created_at < NOW() - INTERVAL '30 days';
END;
$$ LANGUAGE plpgsql;