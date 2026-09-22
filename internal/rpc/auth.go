package rpc

import (
	"context"
	"errors"
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

// streamAuthInterceptor authenticates every stream before its handler
// runs, whatever method it is for.
//
// This is deliberately not a check inside ExecuteOperation. An interceptor
// protects the one RPC that exists today *and* the next one, whose author
// will not think to add a check; a per-handler check protects only what
// someone remembered. It is also why a nil Authenticator refuses
// everything rather than waving it through: the failure mode of a
// misconfigured server should be "nothing works", not "everything is
// open".
func streamAuthInterceptor(auth Authenticator) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if auth == nil {
			return status.Error(codes.Unauthenticated, "rpc: no authenticator configured")
		}

		ctx := ss.Context()
		token, operationID, err := credentialFromContext(ctx)
		if err != nil {
			return status.Error(codes.Unauthenticated, err.Error())
		}

		if err := auth.Authenticate(ctx, token, operationID); err != nil {
			if errors.Is(err, ErrNotBound) {
				return status.Error(codes.PermissionDenied, err.Error())
			}
			return status.Error(codes.Unauthenticated, err.Error())
		}

		return handler(srv, &wrappedStream{
			ServerStream: ss,
			ctx:          context.WithValue(ctx, operationIDKey{}, operationID),
		})
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
