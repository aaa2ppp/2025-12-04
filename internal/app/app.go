package app

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"link-checker/internal/api"
	"link-checker/internal/checker"
	"link-checker/internal/config"
	"link-checker/internal/report"
	"link-checker/internal/server"
	"link-checker/internal/service"
	"link-checker/internal/storage"
)

type App struct {
	// TODO
}

func (a *App) Run(cfg config.Config) error {
	storage, err := storage.Open(prepareStorageConfig(cfg))
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer storage.Close()

	checker := checker.New(prepareCheckerConfig(cfg))

	builder := report.NewBuilder(prepareBuilderConfig(cfg))

	service := service.New(storage, checker, builder)

	api := api.New(service)

	srv := server.New(prepareServerConfig(cfg), api)

	done := make(chan error)
	go func() {
		defer close(done)
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		s := <-c
		_ = s

		if err := srv.Shutdown(); err != nil {
			done <- err
		}
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server: %w", err)
	}

	if err := <-done; err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}

func prepareStorageConfig(cfg config.Config) storage.Config {
	// TODO
	return storage.Config{}
}

func prepareCheckerConfig(cfg config.Config) checker.Config {
	// TODO
	return checker.Config{}
}

func prepareBuilderConfig(cfg config.Config) report.Config {
	// TODO
	return report.Config{}
}

func prepareServerConfig(cfg config.Config) server.Config {
	// TODO
	return server.Config{
		Addr: "127.0.0.1:8080",
	}
}
