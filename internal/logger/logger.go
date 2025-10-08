package logger

import (
	"log/slog"
	"os"
)

type Config struct {
	Level string `json:"level" koanf:"level"`
}

// Provide creates a slog logger.
func Provide(cfg Config) *slog.Logger {
	var level slog.Level

	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// nolint: exhaustruct
	opts := &slog.HandlerOptions{
		Level: level,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, opts))

	return logger
}
