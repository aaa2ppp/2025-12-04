package config

import (
	"log/slog"

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
	defer osExitIfPrintConfig() // XXX

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
			DataFile: ge.String("BBOLT_DATA_FILE", Required, ""),
			MaxCache: ge.Int("BBOLT_MAX_CACHE", !Required, bboltStor.DefaultMaxCache),
			Timeout:  ge.Duration("BBOLT_TIMEOUT", !Required, bboltStor.DefaultTimeout),
			NoSync:   ge.Bool("BBOLT_NOSYNC", !Required, bboltStor.DefaultNoSync),
		},
	}

	if err := ge.Err(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}
