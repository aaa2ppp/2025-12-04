package logger

import (
	"context"
	"log/slog"
)

type loggerKey struct{}

// Context returns context with logger.
func Context(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, log)
}

// FromContext returns the logger from the context. If there is no logger
// in the context, it returns the default logger.
func FromContext(ctx context.Context) *slog.Logger {
	log := ctx.Value(loggerKey{})
	if log != nil {
		return log.(*slog.Logger)
	}
	return slog.Default()
}
