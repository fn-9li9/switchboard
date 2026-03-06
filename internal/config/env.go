package config

import (
	"os"
	"strconv"
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
