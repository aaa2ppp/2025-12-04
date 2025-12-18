package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"link-checker/internal/api"
	"link-checker/internal/checker"
	"link-checker/internal/config"
	"link-checker/internal/logger"
	"link-checker/internal/report/pdf"
	"link-checker/internal/server"
	"link-checker/internal/service"
	"link-checker/internal/storage/bbolt"
)

type App struct {
	// TODO
}

func (a *App) Run(ctx context.Context, cfg config.Config) (anyErr error) {
	logger.SetupDefault(cfg.Logger)

	storage, err := bbolt.Open(prepareStorageConfig(cfg))
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer func() {
		if err := storage.Close(context.Background()); err != nil {
			slog.Error("close storage", "error", err)
			if anyErr == nil {
				anyErr = fmt.Errorf("close storage: %w", err)
			}
		}
	}()

	checker := checker.New(prepareCheckerConfig(cfg))

	builder := pdf.NewBuilder(prepareBuilderConfig(cfg))

	service := service.New(storage, checker, builder)

	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", api.New(service)))
	mux.Handle("/ping", http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "pong", http.StatusOK) }))

	srv := server.New(
		prepareServerConfig(cfg),
		logger.HTTPLogging(slog.Default(), mux),
	)

	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe()
	}()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server: %w", err)
		}
	case <-ctx.Done():
		if err := srv.Shutdown(); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	}

	return nil
}

func prepareStorageConfig(cfg config.Config) bbolt.Config {
	return cfg.BBoltStor
}

func prepareCheckerConfig(cfg config.Config) checker.Config {
	return cfg.Checker
}

func prepareBuilderConfig(cfg config.Config) pdf.Config {
	// TODO
	return pdf.Config{}
}

func prepareServerConfig(cfg config.Config) server.Config {
	return cfg.Server
}
