package rpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/ivanvc/turnip/internal/grpc/turnip/v1"
)

// Polling bounds for the assertions that wait on a gRPC handler
// goroutine.
const (
	waitTimeout = 5 * time.Second
	waitTick    = time.Millisecond
)

// recordingAuth captures what the interceptor passed it and answers with
// a scripted error.
type recordingAuth struct {
	mu          sync.Mutex
	token       string
	operationID string
	calls       int
	err         error
}

func (a *recordingAuth) Authenticate(_ context.Context, token, operationID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	a.token, a.operationID = token, operationID
	return a.err
}

func (a *recordingAuth) snapshot() (token, operationID string, calls int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.token, a.operationID, a.calls
}

// serveWith starts a Server with the given options and returns both a
// typed OperationService client and the raw connection, so a test can
// also open a stream for a method this package never registered.
func serveWith(t *testing.T, handler OperationHandler, opts ...Option) (pb.OperationServiceClient, *grpc.ClientConn) {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	server := NewServer(handler, opts...)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return pb.NewOperationServiceClient(conn), conn
}

// openAndFinish opens a stream, sends one result, and returns whatever
// error surfaced. A rejection by the interceptor arrives on the first
// operation that waits for the server, which CloseAndRecv always does.
func openAndFinish(ctx context.Context, client pb.OperationServiceClient) error {
	stream, err := client.ExecuteOperation(ctx)
	if err != nil {
		return err
	}
	if err := stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Result{Result: &pb.OperationResult{Success: true}},
	}); err != nil {
		return err
	}
	_, err = stream.CloseAndRecv()
	return err
}

func TestInterceptor_RefusesMalformedCredentials(t *testing.T) {
	for name, md := range map[string]metadata.MD{
		"no metadata at all":     {},
		"no authorization":       metadata.Pairs(OperationIDKey, "op-1"),
		"no operation id":        metadata.Pairs(AuthorizationKey, BearerPrefix+"t"),
		"not a bearer token":     metadata.Pairs(AuthorizationKey, "Basic abc", OperationIDKey, "op-1"),
		"empty bearer token":     metadata.Pairs(AuthorizationKey, BearerPrefix, OperationIDKey, "op-1"),
		"empty operation id":     metadata.Pairs(AuthorizationKey, BearerPrefix+"t", OperationIDKey, ""),
		"two authorizations":     metadata.Pairs(AuthorizationKey, BearerPrefix+"a", AuthorizationKey, BearerPrefix+"b", OperationIDKey, "op-1"),
		"two operation ids":      metadata.Pairs(AuthorizationKey, BearerPrefix+"t", OperationIDKey, "op-1", OperationIDKey, "op-2"),
		"authorization reversed": metadata.Pairs(AuthorizationKey, "t "+BearerPrefix, OperationIDKey, "op-1"),
	} {
		t.Run(name, func(t *testing.T) {
			auth := &recordingAuth{}
			handler := &fakeHandler{}
			client, _ := serveWith(t, handler, WithAuthenticator(auth))

			err := openAndFinish(metadata.NewOutgoingContext(context.Background(), md), client)
			require.Error(t, err)
			assert.Equal(t, codes.Unauthenticated, status.Code(err))

			_, _, calls := auth.snapshot()
			assert.Zero(t, calls, "a malformed credential is refused before the authenticator is consulted")
			handler.mu.Lock()
			assert.Empty(t, handler.calls, "nothing reached the handler")
			handler.mu.Unlock()
		})
	}
}

// A Server built without an authenticator refuses every stream rather
// than accepting every stream. Misconfiguration should mean "nothing
// works", not "everything is open".
func TestInterceptor_NoAuthenticatorFailsClosed(t *testing.T) {
	handler := &fakeHandler{}
	client, _ := serveWith(t, handler)

	err := openAndFinish(authedContext("op-1"), client)
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))

	handler.mu.Lock()
	defer handler.mu.Unlock()
	assert.Empty(t, handler.calls)
}

// The two failure modes are distinguishable on the wire, because they
// mean different things: not knowing who a caller is, versus knowing and
// finding it is someone else's Runner.
func TestInterceptor_MapsFailureKindsToDistinctCodes(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want codes.Code
	}{
		"unrecognized caller": {fmt.Errorf("%w: expired", ErrInvalidCredential), codes.Unauthenticated},
		"someone else's operation": {
			fmt.Errorf("%w: pod mismatch", ErrNotBound), codes.PermissionDenied,
		},
	} {
		t.Run(name, func(t *testing.T) {
			handler := &fakeHandler{}
			client, _ := serveWith(t, handler, WithAuthenticator(&recordingAuth{err: tc.err}))

			err := openAndFinish(authedContext("op-1"), client)
			require.Error(t, err)
			assert.Equal(t, tc.want, status.Code(err))

			handler.mu.Lock()
			defer handler.mu.Unlock()
			assert.Empty(t, handler.calls, "a refused stream never reaches the handler")
		})
	}
}

// The interceptor hands the authenticator exactly what the caller
// presented — not a value derived from the stream's contents, which have
// not been read yet.
func TestInterceptor_PassesPresentedCredentialThrough(t *testing.T) {
	auth := &recordingAuth{}
	client, _ := serveWith(t, &fakeHandler{}, WithAuthenticator(auth))

	ctx := metadata.AppendToOutgoingContext(context.Background(),
		AuthorizationKey, BearerPrefix+"the-token",
		OperationIDKey, "op-42",
	)
	require.NoError(t, openAndFinish(ctx, client))

	token, operationID, calls := auth.snapshot()
	assert.Equal(t, "the-token", token)
	assert.Equal(t, "op-42", operationID)
	assert.Equal(t, 1, calls, "authenticated once per stream, not once per message")
}

// The id the handler acts on is the one the interceptor established, not
// the one the Start message names. This is the assignment that used to
// exist in ExecuteOperation, and restoring it — as a simplification, by
// someone who saw a field going unused — would reopen the whole gap.
func TestExecuteOperation_AuthenticatedIDSupersedesTheMessage(t *testing.T) {
	handler := &fakeHandler{}
	client, _ := serveWith(t, handler, WithAuthenticator(&recordingAuth{}))

	stream, err := client.ExecuteOperation(authedContext("op-authenticated"))
	require.NoError(t, err)
	require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Start{Start: &pb.OperationStart{OperationId: "op-claimed"}},
	}))
	require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Result{Result: &pb.OperationResult{Success: true}},
	}))
	_, err = stream.CloseAndRecv()
	require.NoError(t, err)

	handler.mu.Lock()
	defer handler.mu.Unlock()
	require.Len(t, handler.calls, 1)
	assert.Equal(t, "op-authenticated", handler.calls[0].operationID,
		"the message's own operation_id is decorative")
}

// probeDesc is a service this package does not implement and whose
// handler performs no check of its own. Registering it on a Server built
// by NewServer is the assertion that enforcement is structural: an RPC
// added later is authenticated because of where the check lives, not
// because its author remembered to add one.
func probeDesc(record func(ctx context.Context)) *grpc.ServiceDesc {
	return &grpc.ServiceDesc{
		ServiceName: "turnip.test.v1.Probe",
		HandlerType: (*any)(nil),
		Streams: []grpc.StreamDesc{{
			StreamName: "Open",
			Handler: func(_ any, stream grpc.ServerStream) error {
				record(stream.Context())
				return nil
			},
			ServerStreams: true,
			ClientStreams: true,
		}},
	}
}

// probeUnaryDesc is a unary method this package does not implement, whose
// handler performs no check of its own.
//
// It exists because the streaming probe below passed for a server that
// authenticated streams and nothing else. "An RPC added later is
// authenticated" has to be asserted for both kinds, or it is a claim
// about whichever kind the test happened to use.
func probeUnaryDesc(record func(ctx context.Context)) *grpc.ServiceDesc {
	return &grpc.ServiceDesc{
		ServiceName: "turnip.test.v1.UnaryProbe",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Ping",
			Handler: func(_ any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
				in := &pb.ExecuteOperationResponse{}
				if err := dec(in); err != nil {
					return nil, err
				}
				h := func(ctx context.Context, _ any) (any, error) {
					record(ctx)
					return &pb.ExecuteOperationResponse{}, nil
				}
				if interceptor == nil {
					return h(ctx, in)
				}
				return interceptor(ctx, in, &grpc.UnaryServerInfo{FullMethod: "/turnip.test.v1.UnaryProbe/Ping"}, h)
			},
		}},
	}
}

func TestInterceptor_CoversAUnaryRPCItWasNotWrittenFor(t *testing.T) {
	var (
		mu      sync.Mutex
		reached int
		gotID   string
	)
	record := func(ctx context.Context) {
		mu.Lock()
		defer mu.Unlock()
		reached++
		gotID, _ = OperationIDFromContext(ctx)
	}

	lis := bufconn.Listen(1024 * 1024)
	server := NewServer(&fakeHandler{}, WithAuthenticator(&recordingAuth{}))
	server.RegisterService(probeUnaryDesc(record), struct{}{})
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	// Unauthenticated: refused, though the method's handler contains no
	// check whatsoever.
	err = conn.Invoke(context.Background(), "/turnip.test.v1.UnaryProbe/Ping",
		&pb.ExecuteOperationResponse{}, &pb.ExecuteOperationResponse{})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	mu.Lock()
	assert.Zero(t, reached, "a unary RPC nobody protected is still protected")
	mu.Unlock()

	// Authenticated: reaches the handler, and is handed the established
	// Operation id it never asked for.
	err = conn.Invoke(authedContext("op-unary"), "/turnip.test.v1.UnaryProbe/Ping",
		&pb.ExecuteOperationResponse{}, &pb.ExecuteOperationResponse{})
	require.NoError(t, err)
	mu.Lock()
	assert.Equal(t, 1, reached)
	assert.Equal(t, "op-unary", gotID)
	mu.Unlock()
}

func TestInterceptor_CoversAnRPCItWasNotWrittenFor(t *testing.T) {
	var (
		mu      sync.Mutex
		reached int
		gotID   string
	)
	record := func(ctx context.Context) {
		mu.Lock()
		defer mu.Unlock()
		reached++
		gotID, _ = OperationIDFromContext(ctx)
	}

	lis := bufconn.Listen(1024 * 1024)
	server := NewServer(&fakeHandler{}, WithAuthenticator(&recordingAuth{}))
	server.RegisterService(probeDesc(record), struct{}{})
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	desc := &grpc.StreamDesc{StreamName: "Open", ServerStreams: true, ClientStreams: true}

	// Unauthenticated: refused, though the method's handler contains no
	// check whatsoever.
	stream, err := conn.NewStream(context.Background(), desc, "/turnip.test.v1.Probe/Open")
	require.NoError(t, err)
	// The refusal surfaces on the first receive, not on NewStream, which
	// only sets the stream up locally.
	err = stream.RecvMsg(&pb.ExecuteOperationResponse{})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	mu.Lock()
	assert.Zero(t, reached, "an RPC nobody protected is still protected")
	mu.Unlock()

	// Authenticated: reaches the handler, and the handler is handed the
	// established Operation id it never asked for.
	stream, err = conn.NewStream(authedContext("op-probe"), desc, "/turnip.test.v1.Probe/Open")
	require.NoError(t, err)
	// The probe handler returns immediately, so a clean end of stream is
	// what success looks like here.
	require.ErrorIs(t, stream.RecvMsg(&pb.ExecuteOperationResponse{}), io.EOF)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return reached == 1
	}, waitTimeout, waitTick)
	mu.Lock()
	assert.Equal(t, "op-probe", gotID)
	mu.Unlock()
}

// The credential endpoint serves the Operation the caller was
// authenticated as, and there is nothing in the request that could say
// otherwise. This is the same property the streaming forgery test
// asserts, on the endpoint where getting it wrong hands over a
// repository credential rather than a forged result.
func TestFetchCloneCredential_ServesTheAuthenticatedOperation(t *testing.T) {
	handler := &fakeHandler{}
	client, _ := serveWith(t, handler, WithAuthenticator(&recordingAuth{}))

	resp, err := client.FetchCloneCredential(authedContext("op-authenticated"), &pb.FetchCloneCredentialRequest{})
	require.NoError(t, err)
	assert.Equal(t, "ghs_for_op-authenticated", resp.GetToken())

	handler.mu.Lock()
	defer handler.mu.Unlock()
	assert.Equal(t, []string{"op-authenticated"}, handler.credentialFor)
}

func TestFetchCloneCredential_RefusedWithoutACredential(t *testing.T) {
	handler := &fakeHandler{}
	client, _ := serveWith(t, handler, WithAuthenticator(&recordingAuth{}))

	_, err := client.FetchCloneCredential(context.Background(), &pb.FetchCloneCredentialRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))

	handler.mu.Lock()
	defer handler.mu.Unlock()
	assert.Empty(t, handler.credentialFor, "no credential is minted for an unauthenticated caller")
}

// A caller whose credential is valid but belongs to another Operation is
// refused before any token is minted — the cross-repository theft the
// slice's Requirement 2.3 exists to prevent.
func TestFetchCloneCredential_RefusedWhenBoundToAnotherOperation(t *testing.T) {
	handler := &fakeHandler{}
	client, _ := serveWith(t, handler,
		WithAuthenticator(&recordingAuth{err: fmt.Errorf("%w: pod mismatch", ErrNotBound)}))

	_, err := client.FetchCloneCredential(authedContext("op-someone-elses"), &pb.FetchCloneCredentialRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))

	handler.mu.Lock()
	defer handler.mu.Unlock()
	assert.Empty(t, handler.credentialFor)
}

// A minting failure is not leaked verbatim to the Runner: it can name
// repositories the caller was refused for.
func TestFetchCloneCredential_MintFailureIsNotDetailedToTheCaller(t *testing.T) {
	handler := &fakeHandler{credentialErr: errors.New("422 repository not covered: secret-repo")}
	client, _ := serveWith(t, handler, WithAuthenticator(&recordingAuth{}))

	_, err := client.FetchCloneCredential(authedContext("op-1"), &pb.FetchCloneCredentialRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.NotContains(t, err.Error(), "secret-repo")
}
