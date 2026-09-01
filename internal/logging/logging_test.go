package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want slog.Level
	}{
		{"debug", "debug", slog.LevelDebug},
		{"info", "info", slog.LevelInfo},
		{"warn", "warn", slog.LevelWarn},
		{"error", "error", slog.LevelError},
		{"mixed case", "WARN", slog.LevelWarn},
		{"mixed case 2", "Error", slog.LevelError},
		{"empty defaults to info", "", slog.LevelInfo},
		{"unrecognized defaults to info", "verbose", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseLevel(tt.raw))
		})
	}
}

func TestNew(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, slog.LevelInfo)

	logger.Debug("this should not appear")
	assert.Empty(t, buf.String())

	logger.Info("this should appear", "key", "value")

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	assert.Equal(t, "this should appear", record["msg"])
	assert.Equal(t, "INFO", record["level"])
	assert.Equal(t, "value", record["key"])
}
