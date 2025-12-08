package main

import (
	"log"
	"log/slog"
	"os"

	"link-checker/internal/app"
	"link-checker/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	setupLogger()
	app := app.App{}

	if err := app.Run(cfg); err != nil {
		log.Fatal(err)
	}
}

func setupLogger() {
	level := slog.LevelInfo
	switch os.Getenv("LOG_LEVEL") {
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
	slog.Info("logger", "level", level)
}
