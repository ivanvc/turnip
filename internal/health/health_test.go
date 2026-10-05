package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	Healthz().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestReadyz(t *testing.T) {
	tests := []struct {
		name       string
		pingErr    error
		wantStatus int
	}{
		{name: "ping succeeds", pingErr: nil, wantStatus: http.StatusOK},
		{name: "ping fails", pingErr: errors.New("connection refused"), wantStatus: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ping := func(ctx context.Context) error { return tt.pingErr }
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/readyz", nil)

			Readyz(ping, RedisTransport{Addr: "redis:6379", Auth: "none"}).ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestReadyz_BodyTLSPassword(t *testing.T) {
	const secret = "s3cret-pw"
	transport := RedisTransport{Addr: "redis.example:6380", TLS: true, Auth: "password", CustomCA: true}
	tests := []struct {
		name       string
		pingErr    error
		wantStatus int
	}{
		{"200", nil, http.StatusOK},
		{"503", errors.New("dial rediss://:" + secret + "@redis.example:6380 refused"), http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ping := func(context.Context) error { return tt.pingErr }
			rec := httptest.NewRecorder()
			Readyz(ping, transport).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			assert.NotContains(t, rec.Body.String(), secret)
			assert.NotContains(t, rec.Body.String(), "refused")

			var raw map[string]map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
			assert.Equal(t, map[string]any{
				"addr": "redis.example:6380", "tls": true, "auth": "password", "custom_ca": true,
			}, raw["redis"])
			assert.Len(t, raw, 1)
		})
	}
}
