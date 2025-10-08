package logger

import (
	"log/slog"

	"github.com/pterm/pterm"
)

type Config struct {
	Level string `json:"level" koanf:"level"`
}

// Provide creates a slog logger with pterm integration.
func Provide(cfg Config) *slog.Logger {
	var ptermLevel pterm.LogLevel

	switch cfg.Level {
	case "debug":
		ptermLevel = pterm.LogLevelDebug
	case "info":
		ptermLevel = pterm.LogLevelInfo
	case "warn":
		ptermLevel = pterm.LogLevelWarn
	case "error":
		ptermLevel = pterm.LogLevelError
	default:
		ptermLevel = pterm.LogLevelInfo
	}

	// Create a pterm logger
	ptermLogger := pterm.DefaultLogger.
		WithLevel(ptermLevel).
		WithCaller(false)

	// Use pterm's slog handler for colorful output
	handler := pterm.NewSlogHandler(ptermLogger)

	logger := slog.New(handler)

	return logger
}
