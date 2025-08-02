package logging

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// Log is the base logger used throughout the application.
var Log zerolog.Logger

// Init configures the global logger. Log level can be overridden by the
// LOG_LEVEL environment variable (e.g. debug, info, warn, error).
func Init() {
	level := zerolog.InfoLevel
	if lvl := os.Getenv("LOG_LEVEL"); lvl != "" {
		if l, err := zerolog.ParseLevel(strings.ToLower(lvl)); err == nil {
			level = l
		}
	}
	zerolog.TimeFieldFormat = time.RFC3339
	Log = zerolog.New(os.Stdout).Level(level).With().Timestamp().Logger()
}

// Context returns a new context with a request scoped logger containing a
// generated trace_id field.
func Context(ctx context.Context) context.Context {
	logger := Log.With().Str("trace_id", uuid.NewString()).Logger()
	return logger.WithContext(ctx)
}

// WithUser attaches the user id to the logger stored in ctx.
func WithUser(ctx context.Context, userID int64) context.Context {
	logger := zerolog.Ctx(ctx).With().Int64("user_id", userID).Logger()
	return logger.WithContext(ctx)
}

// Ctx extracts the logger from the context or returns the base logger.
func Ctx(ctx context.Context) *zerolog.Logger {
	if l := zerolog.Ctx(ctx); l != nil {
		return l
	}
	return &Log
}

// Snippet returns the first n characters of s.
func Snippet(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
