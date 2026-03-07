package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// ── Structs ────────────────────────────────────────────────────

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

type PostgresConfig struct {
	DSN         string `mapstructure:"dsn"`
	MaxConns    int    `mapstructure:"max_conns"`
	MinConns    int    `mapstructure:"min_conns"`
	MaxIdleSecs int    `mapstructure:"max_idle_secs"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type NATSConfig struct {
	URL string `mapstructure:"url"`
}

type KafkaConfig struct {
	Brokers []string `mapstructure:"brokers"`
	GroupID string   `mapstructure:"group_id"`
}

type AuthConfig struct {
	SessionSecret   string        `mapstructure:"session_secret"`
	EncryptionKey   string        `mapstructure:"encryption_key"`
	SessionDuration time.Duration `mapstructure:"session_duration"`
	MFAIssuer       string        `mapstructure:"mfa_issuer"`
}

type GoogleConfig struct {
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	RedirectURI  string `mapstructure:"redirect_uri"`
}

type TurnstileConfig struct {
	SecretKey string `mapstructure:"secret_key"`
	SiteKey   string `mapstructure:"site_key"`
}

type SMTPConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`
	AppURL   string `mapstructure:"app_url"`
}

type AppConfig struct {
	URL string `mapstructure:"url"`
}

// ── Config raíz ────────────────────────────────────────────────

type Config struct {
	Env          string          `mapstructure:"env"`
	Service      string          `mapstructure:"service"`
	Server       ServerConfig    `mapstructure:"server"`
	Postgres     PostgresConfig  `mapstructure:"postgres"`
	Redis        RedisConfig     `mapstructure:"redis"`
	NATS         NATSConfig      `mapstructure:"nats"`
	Kafka        KafkaConfig     `mapstructure:"kafka"`
	NotifierURL  string          `mapstructure:"notifier_url"`
	ProcessorURL string          `mapstructure:"processor_url"`
	Auth         AuthConfig      `mapstructure:"auth"`
	Google       GoogleConfig    `mapstructure:"google"`
	Turnstile    TurnstileConfig `mapstructure:"turnstile"`
	SMTP         SMTPConfig      `mapstructure:"smtp"`
	App          AppConfig       `mapstructure:"app"`
}

// AppURL es un helper para acceder a la URL de la app desde cualquier lugar.
func (c *Config) AppURL() string {
	return c.App.URL
}

// ── Loader ─────────────────────────────────────────────────────

// Load lee .env + configs/<service>.yaml y mapea env vars con prefijo
// del servicio (GATEWAY_*, PROCESSOR_*, NOTIFIER_*).
//
// Precedencia (mayor a menor):
//  1. Variables de entorno del sistema
//  2. Variables del .env
//  3. configs/<service>.yaml
//  4. Defaults del código
//
// Mapeo de env vars (prefijo = servicio en mayúsculas):
//
//	GATEWAY_AUTH_SESSION_SECRET   → cfg.Auth.SessionSecret
//	GATEWAY_AUTH_ENCRYPTION_KEY   → cfg.Auth.EncryptionKey
//	GATEWAY_GOOGLE_CLIENT_ID      → cfg.Google.ClientID
//	GATEWAY_GOOGLE_CLIENT_SECRET  → cfg.Google.ClientSecret
//	GATEWAY_GOOGLE_REDIRECT_URI   → cfg.Google.RedirectURI
//	GATEWAY_TURNSTILE_SECRET_KEY  → cfg.Turnstile.SecretKey
//	GATEWAY_TURNSTILE_SITE_KEY    → cfg.Turnstile.SiteKey
//	GATEWAY_SMTP_HOST             → cfg.SMTP.Host
//	GATEWAY_SMTP_PORT             → cfg.SMTP.Port
//	GATEWAY_SMTP_USER             → cfg.SMTP.User
//	GATEWAY_SMTP_PASSWORD         → cfg.SMTP.Password
//	GATEWAY_SMTP_FROM             → cfg.SMTP.From
//	GATEWAY_APP_URL               → cfg.App.URL
//	GATEWAY_POSTGRES_DSN          → cfg.Postgres.DSN
//	GATEWAY_REDIS_PASSWORD        → cfg.Redis.Password
func Load(service string) (*Config, error) {
	v := viper.New()

	v.SetConfigName(service)
	v.SetConfigType("yaml")
	v.AddConfigPath("configs/")
	v.AddConfigPath("../../configs/")

	prefix := strings.ToUpper(service) + "_"

	v.SetEnvPrefix(strings.ToUpper(service))
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Bind explícito para garantizar que viper lea las env vars
	// independientemente de si la key existe en el YAML o no
	binds := map[string]string{
		"auth.session_secret":  prefix + "AUTH_SESSION_SECRET",
		"auth.encryption_key":  prefix + "AUTH_ENCRYPTION_KEY",
		"google.client_id":     prefix + "GOOGLE_CLIENT_ID",
		"google.client_secret": prefix + "GOOGLE_CLIENT_SECRET",
		"google.redirect_uri":  prefix + "GOOGLE_REDIRECT_URI",
		"turnstile.secret_key": prefix + "TURNSTILE_SECRET_KEY",
		"turnstile.site_key":   prefix + "TURNSTILE_SITE_KEY",
		"smtp.host":            prefix + "SMTP_HOST",
		"smtp.port":            prefix + "SMTP_PORT",
		"smtp.user":            prefix + "SMTP_USER",
		"smtp.password":        prefix + "SMTP_PASSWORD",
		"smtp.from":            prefix + "SMTP_FROM",
		"app.url":              prefix + "APP_URL",
		"postgres.dsn":         prefix + "POSTGRES_DSN",
		"redis.password":       prefix + "REDIS_PASSWORD",
	}
	for key, env := range binds {
		v.BindEnv(key, env)
	}

	v.SetDefault("env", AppEnv())
	v.SetDefault("service", service)
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("postgres.max_conns", 10)
	v.SetDefault("postgres.min_conns", 2)
	v.SetDefault("postgres.max_idle_secs", 300)
	v.SetDefault("redis.addr", "localhost:6379")
	v.SetDefault("redis.db", 0)
	v.SetDefault("nats.url", "nats://localhost:4222")
	v.SetDefault("kafka.brokers", []string{"localhost:9092"})
	v.SetDefault("kafka.group_id", "switchboard-"+service)
	v.SetDefault("notifier_url", "http://localhost:8081")
	v.SetDefault("processor_url", "http://localhost:8082")
	v.SetDefault("auth.session_duration", "720h")
	v.SetDefault("auth.mfa_issuer", "switchboard")
	v.SetDefault("smtp.host", "smtp.gmail.com")
	v.SetDefault("smtp.port", 587)
	v.SetDefault("app.url", "http://localhost:8080")

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("config: error leyendo %s.yaml: %w", service, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	if err := validate(&cfg, service); err != nil {
		return nil, err
	}

	return &cfg, nil
}
