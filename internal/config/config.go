package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// ── Structs compartidos ────────────────────────────────────────────────────

type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Host string `mapstructure:"host"`
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

// ── Config raíz ────────────────────────────────────────────────────────────

// Config contiene todos los campos posibles. Cada servicio solo usa
// los que necesita; los demás quedan en zero-value.
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

// ── Loader ─────────────────────────────────────────────────────────────────

// Load lee configs/<service>.yaml y permite overrides con variables de entorno.
//
// Ejemplo de override:
//
//	GATEWAY_SERVER_PORT=9090   → config.Server.Port = 9090
//	PROCESSOR_POSTGRES_DSN=... → config.Postgres.DSN = "..."
//	PROCESSOR_NOTIFIER_URL=... → config.NotifierURL = "..."
//	PROCESSOR_PROCESSOR_URL=... → config.ProcessorURL = "..."
//
// El prefijo es el nombre del servicio en mayúsculas.
func Load(service string) (*Config, error) {
	v := viper.New()

	// Archivo base: configs/<service>.yaml
	v.SetConfigName(service)
	v.SetConfigType("yaml")
	v.AddConfigPath("configs/")
	v.AddConfigPath("../../configs/") // útil si se ejecuta desde cmd/<service>/

	// Defaults para URLs de notifier y processor
	v.SetDefault("notifier_url", "http://localhost:8081")
	v.SetDefault("processor_url", "http://localhost:8082")

	// Overrides por env vars: GATEWAY_SERVER_PORT → server.port
	v.SetEnvPrefix(strings.ToUpper(service))
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Defaults razonables
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

	if err := v.ReadInConfig(); err != nil {
		// Si no existe el archivo YAML usamos solo defaults + env vars.
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("config: error leyendo %s.yaml: %w", service, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: error haciendo unmarshal: %w", err)
	}

	return &cfg, nil
}
