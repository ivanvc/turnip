package rpc

// The gRPC metadata a Runner attaches to every stream it opens, and the
// only thing the Server's interceptor reads to decide who is calling.
//
// Both live here rather than being spelled out on each side, because a
// mismatch between them is not a compile error — it is a Server that
// refuses every Runner, discovered at runtime. One definition, imported
// by both.
const (
	// AuthorizationKey carries the Runner's projected ServiceAccount
	// token, prefixed with BearerPrefix. The conventional HTTP name is
	// used rather than a turnip-specific one so that anything inspecting
	// the stream — a proxy's log, a header-redaction rule — recognises it
	// as a credential without being taught to.
	AuthorizationKey = "authorization"
	BearerPrefix     = "Bearer "

	// OperationIDKey is the Operation the caller claims to be reporting
	// for. It travels in metadata rather than being read from the stream's
	// first message because the interceptor runs before any message is
	// received: binding the credential to the Operation is the decision
	// that admits the stream, so it cannot wait for the stream's contents.
	//
	// It is a claim, not an assertion of fact. The interceptor's job is to
	// establish whether the credential presented alongside it actually
	// belongs to that Operation's Pod.
	OperationIDKey = "turnip-operation-id"
)
