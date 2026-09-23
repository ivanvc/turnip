package rpc

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ErrInvalidCredential and ErrNotBound are the two ways authentication
// fails, and they are kept apart because they mean different things to
// whoever reads the log.
//
// ErrInvalidCredential is "I do not know who you are": no token, an
// expired one, one minted for another audience, or a cluster that cannot
// say which Pod it belongs to. ErrNotBound is "I know exactly who you
// are, and it is not who this Operation belongs to" — the forgery this
// slice exists to stop, and the one worth alerting on.
var (
	ErrInvalidCredential = errors.New("rpc: credential not accepted")
	ErrNotBound          = errors.New("rpc: caller is not this operation's runner")
)

// Authenticator decides whether the holder of token may write to
// operationID, returning nil when it may.
//
// It takes the claimed Operation rather than returning the caller's
// identity for the interceptor to compare, because the comparison is the
// whole decision: an implementation that returned "this is Pod X" and
// left the caller to check would be one refactor away from nobody
// checking. See internal/runnerauth for the Kubernetes implementation.
type Authenticator interface {
	Authenticate(ctx context.Context, token, operationID string) error
}

type operationIDKey struct{}

// OperationIDFromContext returns the Operation the interceptor
// established this stream belongs to, and whether one was established at
// all. A false second return means the stream reached a handler without
// passing authentication, which cannot happen while the interceptor is
// installed — handlers treat it as a bug rather than a case to recover
// from.
func OperationIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(operationIDKey{}).(string)
	return id, ok
}

// wrappedStream replaces a ServerStream's context so a handler sees the
// authenticated Operation id. grpc.ServerStream exposes its context
// read-only, so there is no way to do this but to wrap.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }

// authorize is the whole check, shared by both interceptors so the two
// kinds of RPC cannot drift apart.
//
// It returns the context a handler should see: the caller's, carrying the
// Operation the credential was established to belong to.
//
// A nil Authenticator refuses everything rather than waving it through:
// the failure mode of a misconfigured server should be "nothing works",
// not "everything is open".
func authorize(ctx context.Context, auth Authenticator, method string) (context.Context, error) {
	if auth == nil {
		slog.ErrorContext(ctx, "refusing rpc: no authenticator configured", "method", method)
		return nil, status.Error(codes.Unauthenticated, "rpc: no authenticator configured")
	}

	token, operationID, err := credentialFromContext(ctx)
	if err != nil {
		slog.WarnContext(ctx, "refusing rpc: malformed credential", "method", method, "error", err)
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}

	if err := auth.Authenticate(ctx, token, operationID); err != nil {
		if errors.Is(err, ErrNotBound) {
			// Someone presented a valid credential for a different
			// Operation. That is the forgery this check exists for, and
			// it is the one refusal worth alerting on.
			slog.ErrorContext(ctx, "refusing rpc: caller is not this operation's runner",
				"method", method, "claimed_operation", operationID, "error", err)
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		slog.WarnContext(ctx, "refusing rpc: credential not accepted",
			"method", method, "claimed_operation", operationID, "error", err)
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}

	return context.WithValue(ctx, operationIDKey{}, operationID), nil
}

// streamAuthInterceptor authenticates every stream before its handler
// runs, whatever method it is for.
//
// This is deliberately not a check inside ExecuteOperation. An interceptor
// protects the one RPC that exists today *and* the next one, whose author
// will not think to add a check; a per-handler check protects only what
// someone remembered.
func streamAuthInterceptor(auth Authenticator) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, err := authorize(ss.Context(), auth, info.FullMethod)
		if err != nil {
			return err
		}
		return handler(srv, &wrappedStream{ServerStream: ss, ctx: ctx})
	}
}

// unaryAuthInterceptor is the same check for unary methods.
//
// It exists because Slice 25 installed only the stream interceptor, which
// made its own Requirement 4.2 — "an RPC added later is authenticated
// without its author having to remember" — true for streaming RPCs and
// quietly false for unary ones. The probe test that was supposed to prove
// the claim registered a streaming method, so nothing caught it. Slice 24
// needed a unary endpoint and found the hole.
func unaryAuthInterceptor(auth Authenticator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, err := authorize(ctx, auth, info.FullMethod)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// credentialFromContext extracts the bearer token and the claimed
// Operation id from a stream's metadata. Exactly one value of each is
// required: a caller sending two authorization headers is not a caller
// whose first one should be picked.
func credentialFromContext(ctx context.Context) (token, operationID string, err error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", "", errors.New("rpc: no metadata on stream")
	}

	auth := md.Get(AuthorizationKey)
	if len(auth) != 1 {
		return "", "", errors.New("rpc: expected exactly one authorization value")
	}
	if !strings.HasPrefix(auth[0], BearerPrefix) {
		return "", "", errors.New("rpc: authorization is not a bearer credential")
	}
	token = strings.TrimPrefix(auth[0], BearerPrefix)
	if token == "" {
		return "", "", errors.New("rpc: empty bearer credential")
	}

	ids := md.Get(OperationIDKey)
	if len(ids) != 1 || ids[0] == "" {
		return "", "", errors.New("rpc: expected exactly one operation id")
	}

	return token, ids[0], nil
}
