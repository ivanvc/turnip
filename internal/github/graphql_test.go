package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestGraphQLClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Client{graphQLURL: server.URL}
}

func TestClient_MinimizeComment_RequestShape(t *testing.T) {
	var gotReq graphQLRequest
	client := newTestGraphQLClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotReq))
		writeJSON(t, w, graphQLResponse{})
	})

	err := client.MinimizeComment(t.Context(), "IC_kwDOexample")
	require.NoError(t, err)

	assert.Contains(t, gotReq.Query, "minimizeComment")
	assert.Contains(t, gotReq.Query, "OUTDATED")
	assert.Equal(t, "IC_kwDOexample", gotReq.Variables["id"])
}

func TestClient_MinimizeComment_GraphQLError(t *testing.T) {
	client := newTestGraphQLClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, graphQLResponse{Errors: []graphQLError{{Message: "Could not resolve to a node"}}})
	})

	err := client.MinimizeComment(t.Context(), "deleted-node-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Could not resolve to a node")
}
