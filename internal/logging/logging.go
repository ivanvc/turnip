package logging

import (
	"io"
	"log/slog"
	"strings"
)

// ParseLevel maps a TURNIP_LOG_LEVEL value ("debug"|"info"|"warn"|"error",
// case-insensitive) to a slog.Level, defaulting to slog.LevelInfo for an
// empty or unrecognized value rather than erroring — a typo'd log level
// should degrade to a sane default, not stop the process from starting.
func ParseLevel(raw string) slog.Level {
	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// New returns a JSON-handler slog.Logger writing to w at the given level.
func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}
