package logger

import (
	"log/slog"
	"os"
)

type Config struct {
	Level     slog.Level
	Plaintext bool
}

func New(cfg Config) *slog.Logger {
	out := os.Stderr
	ops := &slog.HandlerOptions{Level: cfg.Level}
	if cfg.Plaintext {
		return slog.New(slog.NewTextHandler(out, ops))
	} else {
		return slog.New(slog.NewJSONHandler(out, ops))
	}
}

func SetupDefault(cfg Config) {
	slog.SetDefault(New(cfg))
	slog.Info("setup default logger", "level", cfg.Level)
}
