package logger

import (
	"context"
	"log/slog"
)

type loggerKey struct{}

func Context(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, log)
}

func FromContext(ctx context.Context) *slog.Logger {
	log := ctx.Value(loggerKey{})
	if log != nil {
		return log.(*slog.Logger)
	}
	return slog.Default()
}
