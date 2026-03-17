package config

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func InitLogger(env, service string) zerolog.Logger {
	var output io.Writer

	if env == "dev" {
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: "15:04:05",
			NoColor:    false,
		}
	} else {
		output = os.Stdout
	}

	level := zerolog.InfoLevel
	if env == "dev" {
		level = zerolog.DebugLevel
	}

	zerolog.TimeFieldFormat = time.RFC3339

	logger := zerolog.New(output).
		Level(level).
		With().
		Timestamp().
		Str("service", service).
		Logger()

	log.Logger = logger

	return logger
}
