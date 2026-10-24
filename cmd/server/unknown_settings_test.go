package main

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnknownSettings(t *testing.T) {
	cases := []struct {
		name string
		env  []string
		want []string
	}{
		{"unknown name", []string{"TURNIP_RUNNER_TIMEOUT=5m"}, []string{"TURNIP_RUNNER_TIMEOUT"}},
		{"setting no longer read", []string{"TURNIP_K8S_NAMESPACE=x"}, []string{"TURNIP_K8S_NAMESPACE"}},
		{"all known", []string{"TURNIP_REDIS_ADDR=a", "TURNIP_HTTP_ADDR=:1", "PATH=/bin"}, nil},
		{"service variables", []string{
			"TURNIP_REDIS_PORT=tcp://10.0.0.1:6379",
			"TURNIP_REDIS_SERVICE_HOST=10.0.0.1",
			"TURNIP_SERVER_SERVICE_PORT=80",
			"TURNIP_SERVER_SERVICE_PORT_GRPC=9090",
			"TURNIP_SERVER_PORT_9090_TCP_ADDR=10.0.0.2",
			"TURNIP_SERVER_PORT_9090_TCP_PROTO=tcp",
			"TURNIP_SERVER_PORT_9090_TCP_PORT=9090",
			"TURNIP_SERVER_PORT_9090_TCP=tcp://10.0.0.2:9090",
		}, nil},
		{"order kept", []string{"TURNIP_B=1", "TURNIP_REDIS_ADDR=a", "TURNIP_A=1"}, []string{"TURNIP_B", "TURNIP_A"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, unknownSettings(tc.env))
		})
	}
}

func TestWarnUnknownSettings(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	warnUnknownSettings([]string{"TURNIP_REDIS_ADDR=a"})
	assert.Empty(t, buf.String())

	warnUnknownSettings([]string{"TURNIP_X=1", "TURNIP_Y=2"})
	require.Equal(t, 1, bytes.Count(buf.Bytes(), []byte("unknown settings")))
	assert.Contains(t, buf.String(), "level=WARN")
	assert.Contains(t, buf.String(), "TURNIP_X")
	assert.Contains(t, buf.String(), "TURNIP_Y")
}
