package runner

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/ivanvc/turnip/internal/grpc/turnip/v1"
	"github.com/ivanvc/turnip/internal/rpc"
)

// recordingServer accepts every stream and records the metadata each one
// arrived with. It asserts nothing itself: these are handler goroutines,
// where a failed require would call runtime.Goexit without failing the
// test (testifylint's go-require rule).
type recordingServer struct {
	pb.UnimplementedOperationServiceServer

	mu sync.Mutex
	md []metadata.MD
}

func (s *recordingServer) ExecuteOperation(stream pb.OperationService_ExecuteOperationServer) error {
	md, _ := metadata.FromIncomingContext(stream.Context())
	s.mu.Lock()
	s.md = append(s.md, md)
	s.mu.Unlock()

	for {
		if _, err := stream.Recv(); err == io.EOF {
			return stream.SendAndClose(&pb.ExecuteOperationResponse{})
		} else if err != nil {
			return err
		}
	}
}

func (s *recordingServer) snapshot() []metadata.MD {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]metadata.MD(nil), s.md...)
}

func dialRecordingServer(t *testing.T, srv *recordingServer) pb.OperationServiceClient {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	pb.RegisterOperationServiceServer(server, srv)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return pb.NewOperationServiceClient(conn)
}

// Every stream carries the credential and the Operation it claims to be
// reporting for. The Server's interceptor runs before any message is
// received, so anything it needs has to be metadata — a Start message
// naming the Operation arrives too late to admit the stream.
func TestReporter_PresentsCredentialAndOperationID(t *testing.T) {
	srv := &recordingServer{}
	clock := newFakeClock()
	r := newReporter(dialRecordingServer(t, srv), testCfg(), clock.Now, clock.Sleep, func() float64 { return 0 })
	r.readToken = stubToken

	require.NoError(t, r.Connect(context.Background()))
	require.Eventually(t, func() bool { return len(srv.snapshot()) == 1 }, waitTimeout, waitTick)

	md := srv.snapshot()[0]
	assert.Equal(t, []string{rpc.BearerPrefix + "test-token"}, md.Get(rpc.AuthorizationKey))
	assert.Equal(t, []string{"op-1"}, md.Get(rpc.OperationIDKey))
}

// kubelet rotates a projected token in place, so a reconnect minutes into
// a long Operation must present whatever is in the file *now*. The
// assertion is on reads, not on timing: a reporter that cached the token
// at construction would read once and present a stale credential on every
// reconnect thereafter.
func TestReporter_RereadsTokenOnEveryStream(t *testing.T) {
	srv := &recordingServer{}
	clock := newFakeClock()
	r := newReporter(dialRecordingServer(t, srv), testCfg(), clock.Now, clock.Sleep, func() float64 { return 0 })

	var mu sync.Mutex
	reads := 0
	r.readToken = func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		reads++
		return fmt.Sprintf("token-%d", reads), nil
	}

	require.NoError(t, r.openStream(context.Background()))
	require.NoError(t, r.openStream(context.Background()))
	require.Eventually(t, func() bool { return len(srv.snapshot()) == 2 }, waitTimeout, waitTick)

	mu.Lock()
	assert.Equal(t, 2, reads, "the token file is read once per stream, not once per process")
	mu.Unlock()

	// Asserted as a set: each stream is registered by its own handler
	// goroutine, which is not ordered against the client call that opened
	// it returning, so the arrival order carries no information.
	var presented []string
	for _, md := range srv.snapshot() {
		presented = append(presented, md.Get(rpc.AuthorizationKey)...)
	}
	assert.ElementsMatch(t, []string{rpc.BearerPrefix + "token-1", rpc.BearerPrefix + "token-2"}, presented,
		"the second stream presents the rotated token, not the first one again")
}

// A Runner that cannot read its credential opens no stream at all, rather
// than opening one the Server will refuse for reasons the Runner could
// have named itself.
func TestReporter_UnreadableTokenFailsOpenStream(t *testing.T) {
	srv := &recordingServer{}
	clock := newFakeClock()
	r := newReporter(dialRecordingServer(t, srv), testCfg(), clock.Now, clock.Sleep, func() float64 { return 0 })
	r.readToken = func() (string, error) { return "", fmt.Errorf("no such file") }

	require.Error(t, r.openStream(context.Background()))
	assert.Empty(t, srv.snapshot())
}
