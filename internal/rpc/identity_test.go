package rpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/ivanvc/turnip/internal/grpc/turnip/v1"
)

// lockedBuffer is a bytes.Buffer safe to write from the gRPC server's
// goroutines while the test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) snapshot() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.buf.Bytes())
}

// captureLogs points the process-wide slog default at a buffer for the
// rest of the test and returns a function decoding every record written
// so far. Safe only because no test in this package runs in parallel.
func captureLogs(t *testing.T) func() []map[string]any {
	t.Helper()
	buf := &lockedBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	return func() []map[string]any {
		var records []map[string]any
		sc := bufio.NewScanner(bytes.NewReader(buf.snapshot()))
		for sc.Scan() {
			var rec map[string]any
			require.NoError(t, json.Unmarshal(sc.Bytes(), &rec))
			records = append(records, rec)
		}
		return records
	}
}

// authedIncomingContext is authedContext as the Server receives it, for
// calling authorize directly.
func authedIncomingContext(operationID string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		AuthorizationKey, BearerPrefix+"test-token",
		OperationIDKey, operationID,
	))
}

func findRecord(records []map[string]any, msg string) map[string]any {
	for _, r := range records {
		if r["msg"] == msg {
			return r
		}
	}
	return nil
}

// The tool runs in the runner container beside the report identity, so
// the repository's own code can present it. Asking for a GitHub
// credential with it is refused before anything is minted, and logged as
// the attempt it is.
func TestFetchCloneCredential_RefusedForTheReportIdentity(t *testing.T) {
	logs := captureLogs(t)
	handler := &fakeHandler{}
	client, _ := serveWith(t, handler, WithAuthenticator(reportAuth()))

	resp, err := client.FetchCloneCredential(authedContext("op-1"), &pb.FetchCloneCredentialRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, resp.GetToken())

	handler.mu.Lock()
	assert.Empty(t, handler.credentialFor, "CloneCredential is never invoked for the report identity")
	handler.mu.Unlock()

	rec := findRecord(logs(), "refusing rpc: credential fetch requires the clone identity")
	require.NotNil(t, rec, "the refusal is logged")
	assert.Equal(t, "ERROR", rec["level"])
	assert.Equal(t, "op-1", rec["operation_id"])
	assert.Equal(t, pb.OperationService_FetchCloneCredential_FullMethodName, rec["method"])
}

func TestFetchCloneCredential_AdmittedForTheCloneIdentity(t *testing.T) {
	logs := captureLogs(t)
	handler := &fakeHandler{}
	client, _ := serveWith(t, handler, WithAuthenticator(cloneAuth()))

	resp, err := client.FetchCloneCredential(authedContext("op-1"), &pb.FetchCloneCredentialRequest{})
	require.NoError(t, err)
	assert.Equal(t, "ghs_for_op-1", resp.GetToken())

	handler.mu.Lock()
	assert.Equal(t, []string{"op-1"}, handler.credentialFor)
	handler.mu.Unlock()

	assert.Nil(t, findRecord(logs(), "refusing rpc: credential fetch requires the clone identity"))
}

// Both containers report: the clone container a failed clone, the runner
// container the tool's result.
func TestExecuteOperation_AdmittedForEitherIdentity(t *testing.T) {
	for _, identity := range []Identity{IdentityReport, IdentityClone} {
		t.Run(identity.String(), func(t *testing.T) {
			handler := &fakeHandler{}
			client, _ := serveWith(t, handler, WithAuthenticator(&recordingAuth{identity: identity}))

			require.NoError(t, openAndFinish(authedContext("op-1"), client))

			handler.mu.Lock()
			defer handler.mu.Unlock()
			require.Len(t, handler.calls, 1)
			assert.Equal(t, "result", handler.calls[0].kind)
			assert.Equal(t, "op-1", handler.calls[0].operationID)
		})
	}
}

// The interceptor records the identity next to the Operation id, for any
// method, so a handler can decide what that identity may do.
func TestInterceptor_RecordsTheIdentity(t *testing.T) {
	for _, identity := range []Identity{IdentityReport, IdentityClone} {
		t.Run(identity.String(), func(t *testing.T) {
			ctx, err := authorize(authedIncomingContext("op-1"), &recordingAuth{identity: identity}, "/test/Method")
			require.NoError(t, err)

			got, ok := IdentityFromContext(ctx)
			require.True(t, ok)
			assert.Equal(t, identity, got)
			id, ok := OperationIDFromContext(ctx)
			require.True(t, ok)
			assert.Equal(t, "op-1", id)
		})
	}
}

// An Authenticator that admits a caller without naming an identity is
// broken, and the caller is refused on every method rather than treated
// as either identity.
func TestInterceptor_RefusesWhenNoIdentityWasEstablished(t *testing.T) {
	handler := &fakeHandler{}
	client, _ := serveWith(t, handler, WithAuthenticator(&recordingAuth{identity: IdentityNone}))

	err := openAndFinish(authedContext("op-1"), client)
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))

	_, err = client.FetchCloneCredential(authedContext("op-1"), &pb.FetchCloneCredentialRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))

	handler.mu.Lock()
	defer handler.mu.Unlock()
	assert.Empty(t, handler.calls)
	assert.Empty(t, handler.credentialFor)
}

// The handler's own guard, reached without the interceptor: a context
// carrying an Operation id but no identity (or the zero one) mints
// nothing. This is what keeps a refactor that drops the identity from
// defaulting to "allowed".
func TestFetchCloneCredential_NoIdentityInContextFailsClosed(t *testing.T) {
	withID := context.WithValue(context.Background(), operationIDKey{}, "op-1")
	for name, ctx := range map[string]context.Context{
		"no identity recorded":   withID,
		"zero identity recorded": context.WithValue(withID, identityKey{}, IdentityNone),
	} {
		t.Run(name, func(t *testing.T) {
			handler := &fakeHandler{}
			server := &operationServer{handler: handler}

			_, err := server.FetchCloneCredential(ctx, &pb.FetchCloneCredentialRequest{})
			require.Error(t, err)
			assert.Equal(t, codes.Unauthenticated, status.Code(err))

			handler.mu.Lock()
			defer handler.mu.Unlock()
			assert.Empty(t, handler.credentialFor)
		})
	}
}
