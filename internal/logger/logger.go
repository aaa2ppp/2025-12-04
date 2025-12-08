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
	if cfg.Plaintext {
		return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.Level}))
	} else {
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.Level}))
	}
}

func SetupDefault(cfg Config) {
	slog.SetDefault(New(cfg))
}
