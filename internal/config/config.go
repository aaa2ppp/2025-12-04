package config

import (
	"log/slog"
	"time"

	"link-checker/internal/checker"
	"link-checker/internal/logger"
	"link-checker/internal/server"
	bboltStor "link-checker/internal/storage/bbolt"
)

type (
	Logger    = logger.Config
	Server    = server.Config
	Checker   = checker.Config
	BBoltStor = bboltStor.Config
)

type Config struct {
	Logger    Logger
	Server    Server
	Checker   Checker
	BBoltStor BBoltStor
	// TODO
}

func Load() (Config, error) {
	// XXX завершить программу с ошибкой, если установлена переменная окружения PRINT_CONFIG
	defer osExitIfPrintConfig()

	var ge Getenv

	cfg := Config{
		Logger: Logger{
			Level:     ge.LogLevel("LOG_LEVEL", !Required, slog.LevelInfo),
			Plaintext: ge.Bool("LOG_PLAINTEXT", !Required, false),
		},
		Server: Server{
			Addr:            ge.String("SERVER_ADDR", Required, ""),
			ShutdownTimeout: ge.Duration("SERVER_SHUTDOWN_TIMEOUT", !Required, server.DefaultShutdownTimeout),
			RetryAfter:      ge.Int("SERVER_RETRY_AFTER", !Required, server.DefaultRetryAfter),
		},
		Checker: Checker{
			Timeout:        ge.Duration("CHECKER_TIMEOUT", !Required, checker.DefaultTimeout),
			UserAgent:      ge.String("CHECKER_USER_AGENT", !Required, checker.DefaultUserAgent),
			TryHTTPSFirst:  ge.Bool("CHECKER_TRY_HTTPS_FIRST", !Required, false),
			TryGETFallback: ge.Bool("CHECKER_TRY_GET_FALLBACK", !Required, false),
		},
		BBoltStor: BBoltStor{
			DataFile:       ge.String("BBSTOR_DATA_FILE", Required, ""),
			OpenTimeout:    ge.Duration("BBSTOR_OPEN_TIMEOUT", !Required, 1*time.Second),
			CacheSize:      ge.Int("BBSTOR_CACHE_SIZE", !Required, 1024),
			NoSync:         ge.Bool("BBSTOR_NO_SYNC", !Required, false),
			MaxSyncDelay:   ge.Duration("BBSTOR_MAX_SYNC_DELAY", !Required, 0),
			MaxSyncPending: ge.Int("BBSTOR_MAX_SYNC_PENDING", !Required, 0),
			MaxBatchDelay:  ge.Duration("BBSTOR_MAX_BATCH_DELAY", !Required, 10*time.Millisecond),
			MaxBatchSize:   ge.Int("BBSTOR_MAX_BATCH_SIZE", !Required, bboltStor.DefaultMaxBatchSize),
		},
	}

	if err := ge.Err(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}
