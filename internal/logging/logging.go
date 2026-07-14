// Package logging configures structured, leveled application logging
// (distinct from internal/audit's security-event audit trail) using the
// standard library's log/slog. It's intended to make operational logs
// easy to parse and correlate in production (e.g. by a log aggregator),
// while remaining human-readable for local development.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Init configures the process-wide default slog logger from the LOG_FORMAT
// and LOG_LEVEL environment variables and returns it.
//
// LOG_FORMAT selects the output encoding:
//   - "json" (default): one JSON object per line, suitable for log
//     aggregation/parsing in production.
//   - "text": human-readable key=value output, suitable for local
//     development.
//
// LOG_LEVEL selects the minimum level logged: "debug", "info" (default),
// "warn", or "error".
func Init() *slog.Logger {
	level := parseLevel(os.Getenv("LOG_LEVEL"))

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if strings.EqualFold(os.Getenv("LOG_FORMAT"), "text") {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
