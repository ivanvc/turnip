package runner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pb "github.com/ivanvc/turnip/internal/grpc/turnip/v1"
	"github.com/ivanvc/turnip/internal/rpc"
)

// RunCredential answers one git credential request and returns the exit
// code cmd/runner/main.go should use.
//
// git invokes this as `<runner> credential get`, writes the request as
// key=value lines on stdin, and reads the answer the same way from
// stdout. The credential exists in this process's memory and on that
// pipe, and nowhere else: not in a URL, not in an argument, not in a
// file (Slice 24, Requirement 3).
//
// Operations other than "get" are answered with silence. turnip has
// nothing to store and nothing to erase, and a helper that fails on them
// would make git treat a successful clone as broken.
func RunCredential(ctx context.Context, cfg Config, operation string, stdin io.Reader, stdout, stderr io.Writer) int {
	if operation != "get" {
		return 0
	}

	req := parseCredentialRequest(stdin)

	// git asks every configured helper for every host it talks to. This
	// credential is GitHub's and belongs only to the Operation's own
	// host, so anything else gets no answer rather than a token — a
	// submodule pointing at another host must not be handed one.
	want := hostOfRepoURL(cfg.RepoURL)
	if want == "" || !strings.EqualFold(req["host"], want) {
		return 0
	}

	token, err := fetchCloneCredential(ctx, cfg)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "runner: credential: %v\n", err)
		return 1
	}

	// x-access-token is the username GitHub expects alongside an
	// installation token.
	_, _ = fmt.Fprintf(stdout, "username=x-access-token\npassword=%s\n\n", token)
	return 0
}

// fetchCloneCredential asks the Server for this Operation's credential.
//
// The request carries no operation id: the Server decides which
// credential to return from the identity this Pod's projected
// ServiceAccount token establishes. See rpc.FetchCloneCredential.
func fetchCloneCredential(ctx context.Context, cfg Config) (string, error) {
	raw, err := os.ReadFile(cfg.TokenFile)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", cfg.TokenFile, err)
	}

	conn, err := grpc.NewClient(cfg.ServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", fmt.Errorf("dial %s: %w", cfg.ServerAddr, err)
	}
	defer func() { _ = conn.Close() }()

	ctx = metadata.AppendToOutgoingContext(ctx,
		rpc.AuthorizationKey, rpc.BearerPrefix+strings.TrimSpace(string(raw)),
		rpc.OperationIDKey, cfg.OperationID,
	)

	resp, err := pb.NewOperationServiceClient(conn).FetchCloneCredential(ctx, &pb.FetchCloneCredentialRequest{})
	if err != nil {
		return "", fmt.Errorf("fetching clone credential: %w", err)
	}
	if resp.GetToken() == "" {
		return "", fmt.Errorf("server returned an empty credential")
	}
	return resp.GetToken(), nil
}

// parseCredentialRequest reads git's key=value request. Unknown keys are
// kept rather than rejected: git adds them over time, and a helper that
// refuses what it does not recognize ages badly.
func parseCredentialRequest(r io.Reader) map[string]string {
	out := map[string]string{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			out[k] = v
		}
	}
	return out
}

func hostOfRepoURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
