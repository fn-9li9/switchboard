package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Env devuelve la variable de entorno o el valor por defecto si no está definida.
func Env(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// EnvInt devuelve la variable de entorno como int o el valor por defecto.
func EnvInt(key string, defaultVal int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return n
}

// EnvBool devuelve la variable de entorno como bool o el valor por defecto.
func EnvBool(key string, defaultVal bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return defaultVal
	}
	return b
}

// AppEnv devuelve el entorno de ejecución: "dev" o "prod".
// Se controla con la variable APP_ENV.
func AppEnv() string {
	return Env("APP_ENV", "dev")
}

// validate falla rápido si faltan secretos críticos en el gateway.
func validate(cfg *Config, service string) error {
	if service != "gateway" {
		return nil
	}

	type required struct {
		val  string
		name string
	}

	checks := []required{
		{cfg.Auth.SessionSecret, "GATEWAY_AUTH_SESSION_SECRET"},
		{cfg.Auth.EncryptionKey, "GATEWAY_AUTH_ENCRYPTION_KEY"},
		{cfg.Google.ClientID, "GATEWAY_GOOGLE_CLIENT_ID"},
		{cfg.Google.ClientSecret, "GATEWAY_GOOGLE_CLIENT_SECRET"},
		{cfg.Turnstile.SecretKey, "GATEWAY_TURNSTILE_SECRET_KEY"},
		{cfg.Turnstile.SiteKey, "GATEWAY_TURNSTILE_SITE_KEY"},
		{cfg.SMTP.User, "GATEWAY_SMTP_USER"},
		{cfg.SMTP.Password, "GATEWAY_SMTP_PASSWORD"},
		{cfg.SMTP.From, "GATEWAY_SMTP_FROM"},
		{cfg.App.URL, "GATEWAY_APP_URL"},
	}

	var missing []string
	for _, c := range checks {
		if c.val == "" {
			missing = append(missing, c.name)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("config: missing required secrets:\n  %s\n\nCopy .env.example to .env and fill in the values.",
			strings.Join(missing, "\n  "))
	}

	return nil
}
