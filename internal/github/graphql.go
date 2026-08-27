package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const graphQLEndpoint = "https://api.github.com/graphql"

// minimizeCommentMutation marks a comment outdated. classifier is fixed to
// OUTDATED: this package has no other use for minimizeComment (see
// server-orchestration's requirements.md "Out of Scope").
const minimizeCommentMutation = `mutation($id: ID!) { minimizeComment(input: {subjectId: $id, classifier: OUTDATED}) { minimizedComment { isMinimized } } }`

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type graphQLResponse struct {
	Errors []graphQLError `json:"errors"`
}

// MinimizeComment marks the comment identified by nodeID (a GraphQL node
// ID, not the numeric REST comment ID) as outdated. There is no REST
// equivalent for this operation — it only exists as a GraphQL mutation.
func (c *Client) MinimizeComment(ctx context.Context, nodeID string) error {
	payload, err := json.Marshal(graphQLRequest{
		Query:     minimizeCommentMutation,
		Variables: map[string]any{"id": nodeID},
	})
	if err != nil {
		return fmt.Errorf("github: encoding minimizeComment request for %s: %w", nodeID, err)
	}

	url := graphQLEndpoint
	if c.graphQLURL != "" {
		url = c.graphQLURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("github: building minimizeComment request for %s: %w", nodeID, err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Reuses the same installation-authenticated transport the REST client
	// (c.gh) already uses, so no separate credential handling is needed.
	// c.itr must not be assigned into http.Client.Transport when nil: a
	// nil *ghinstallation.Transport stored in the http.RoundTripper
	// interface is a non-nil interface value, so http.Client would call
	// RoundTrip on it instead of falling back to http.DefaultTransport.
	transport := http.DefaultTransport
	if c.itr != nil {
		transport = c.itr
	}
	httpClient := &http.Client{Transport: transport}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("github: minimizing comment %s: %w", nodeID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result graphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("github: decoding minimizeComment response for %s: %w", nodeID, err)
	}
	if len(result.Errors) > 0 {
		return fmt.Errorf("github: minimizing comment %s: %s", nodeID, result.Errors[0].Message)
	}

	return nil
}
