package github

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gh "github.com/google/go-github/v90/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T) (*Client, *http.ServeMux) {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	baseURL := server.URL + "/"
	ghClient, err := gh.NewClient(gh.WithURLs(&baseURL, &baseURL))
	require.NoError(t, err)

	return &Client{gh: ghClient}, mux
}

// writeJSON is called from httptest.Server handlers, which run on their
// own goroutine — use assert, not require, since require's FailNow (via
// runtime.Goexit) doesn't correctly fail the test from a non-test
// goroutine (testifylint's go-require check).
func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	assert.NoError(t, json.NewEncoder(w).Encode(v))
}

func TestClient_GetFile(t *testing.T) {
	client, mux := newTestClient(t)

	mux.HandleFunc("/repos/owner/repo/contents/turnip.yaml", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "main", r.URL.Query().Get("ref"))
		content := base64.StdEncoding.EncodeToString([]byte("version: 1\n"))
		writeJSON(t, w, map[string]string{"type": "file", "encoding": "base64", "content": content})
	})

	got, err := client.GetFile(t.Context(), "owner", "repo", "turnip.yaml", "main")
	require.NoError(t, err)
	assert.Equal(t, "version: 1\n", string(got))
}

func TestClient_GetFile_NotFound(t *testing.T) {
	client, mux := newTestClient(t)

	mux.HandleFunc("/repos/owner/repo/contents/missing.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(t, w, map[string]string{"message": "Not Found"})
	})

	_, err := client.GetFile(t.Context(), "owner", "repo", "missing.yaml", "main")
	assert.ErrorIs(t, err, ErrFileNotFound)
}

func TestClient_GetFile_Directory(t *testing.T) {
	client, mux := newTestClient(t)

	mux.HandleFunc("/repos/owner/repo/contents/somedir", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []map[string]string{{"type": "dir", "name": "nested"}})
	})

	_, err := client.GetFile(t.Context(), "owner", "repo", "somedir", "main")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrFileNotFound)
}

func TestClient_GetModifiedFiles_Paginates(t *testing.T) {
	client, mux := newTestClient(t)

	mux.HandleFunc("/repos/owner/repo/pulls/42/files", func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		switch page {
		case "", "1":
			w.Header().Set("Link", `<https://api.github.com/resource?page=2>; rel="next"`)
			writeJSON(t, w, []map[string]string{{"filename": "a.tf"}, {"filename": "b.tf"}})
		case "2":
			writeJSON(t, w, []map[string]string{{"filename": "c.tf"}})
		default:
			assert.Failf(t, "unexpected page", "page = %q", page)
		}
	})

	got, err := client.GetModifiedFiles(t.Context(), "owner", "repo", 42)
	require.NoError(t, err)
	assert.Equal(t, []string{"a.tf", "b.tf", "c.tf"}, got)
}

func TestClient_GetPullRequest(t *testing.T) {
	client, mux := newTestClient(t)

	mux.HandleFunc("/repos/owner/repo/pulls/7", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"number": 7,
			"head":   map[string]string{"sha": "abc123", "ref": "feature"},
			"base":   map[string]string{"ref": "main"},
		})
	})

	got, err := client.GetPullRequest(t.Context(), "owner", "repo", 7)
	require.NoError(t, err)
	assert.Equal(t, &PullRequest{Number: 7, HeadSHA: "abc123", BaseRef: "main", HeadRef: "feature"}, got)
}

func TestClient_CreateCheckRun(t *testing.T) {
	client, mux := newTestClient(t)

	var gotBody map[string]any
	mux.HandleFunc("/repos/owner/repo/check-runs", func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		writeJSON(t, w, map[string]int64{"id": 99})
	})

	id, err := client.CreateCheckRun(t.Context(), "owner", "repo", CheckRunOptions{
		Name: "plan", HeadSHA: "abc123", Status: "in_progress",
	})
	require.NoError(t, err)
	assert.EqualValues(t, 99, id)
	assert.NotContains(t, gotBody, "conclusion", "want it omitted for an empty CheckRunOptions.Conclusion")
	assert.Equal(t, "in_progress", gotBody["status"])
}

// The four tests below pin GitHub's all-or-nothing `output` contract:
// the object may be omitted, but if present it must carry both `title`
// and `summary`. Violating it fails the whole request with
// `422 ... "summary", "title" weren't supplied`, which is what every
// check run turnip created used to hit.
func TestToCheckRunOutput_OmittedWhenNothingToReport(t *testing.T) {
	// execute.go's creation call: name/headSHA/status only.
	assert.Nil(t, toCheckRunOutput(CheckRunOptions{Name: "turnip/web/diff", HeadSHA: "abc123", Status: "in_progress"}))
}

func TestToCheckRunOutput_TextOnlyStillSuppliesRequiredFields(t *testing.T) {
	// sweep.go's timeout update and execute.go's job-failure update.
	out := toCheckRunOutput(CheckRunOptions{Name: "turnip/web/diff", Status: "completed", Conclusion: "failure", Text: "boom"})
	require.NotNil(t, out)
	assert.Equal(t, "turnip/web/diff", out.GetTitle(), "falls back to the check run's name")
	assert.NotEmpty(t, out.GetSummary())
	assert.Equal(t, "boom", out.GetText())
}

func TestToCheckRunOutput_SummaryWithoutTitleGetsATitle(t *testing.T) {
	// result.go's final-result update: summary and text, no title.
	out := toCheckRunOutput(CheckRunOptions{Name: "turnip/web/diff", Status: "completed", Summary: "1 to change", Text: "diff output"})
	require.NotNil(t, out)
	assert.Equal(t, "turnip/web/diff", out.GetTitle())
	assert.Equal(t, "1 to change", out.GetSummary())
}

func TestClient_CreateCheckRun_OmitsEmptyOutputObject(t *testing.T) {
	client, mux := newTestClient(t)

	var gotBody map[string]any
	mux.HandleFunc("/repos/owner/repo/check-runs", func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		writeJSON(t, w, map[string]int64{"id": 99})
	})

	_, err := client.CreateCheckRun(t.Context(), "owner", "repo", CheckRunOptions{
		Name: "turnip/web/diff", HeadSHA: "abc123", Status: "in_progress",
	})
	require.NoError(t, err)
	assert.NotContains(t, gotBody, "output", `an "output": {} with no title/summary is a 422`)
}

func TestClient_UpdateCheckRun(t *testing.T) {
	client, mux := newTestClient(t)

	var gotBody map[string]any
	mux.HandleFunc("/repos/owner/repo/check-runs/99", func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		writeJSON(t, w, map[string]int64{"id": 99})
	})

	err := client.UpdateCheckRun(t.Context(), "owner", "repo", 99, CheckRunOptions{
		Name: "plan", Status: "completed", Conclusion: "success",
	})
	require.NoError(t, err)
	assert.Equal(t, "success", gotBody["conclusion"])
}

func TestClient_PostComment(t *testing.T) {
	client, mux := newTestClient(t)

	mux.HandleFunc("/repos/owner/repo/issues/5/comments", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"id": 555, "node_id": "IC_kwDOexample"})
	})

	posted, err := client.PostComment(t.Context(), "owner", "repo", 5, "hello")
	require.NoError(t, err)
	assert.EqualValues(t, 555, posted.ID)
	assert.Equal(t, "IC_kwDOexample", posted.NodeID)
}

func TestClient_UpdateComment(t *testing.T) {
	client, mux := newTestClient(t)

	var gotBody map[string]string
	mux.HandleFunc("/repos/owner/repo/issues/comments/555", func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		writeJSON(t, w, map[string]int64{"id": 555})
	})

	require.NoError(t, client.UpdateComment(t.Context(), "owner", "repo", 555, "updated"))
	assert.Equal(t, "updated", gotBody["body"])
}

func TestClient_IsCollaborator(t *testing.T) {
	client, mux := newTestClient(t)

	mux.HandleFunc("/repos/owner/repo/collaborators/alice", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/repos/owner/repo/collaborators/bob", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	ok, err := client.IsCollaborator(t.Context(), "owner", "repo", "alice")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = client.IsCollaborator(t.Context(), "owner", "repo", "bob")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestClient_GetCollaboratorPermission(t *testing.T) {
	client, mux := newTestClient(t)

	mux.HandleFunc("/repos/owner/repo/collaborators/alice/permission", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]string{"permission": "write"})
	})

	got, err := client.GetCollaboratorPermission(t.Context(), "owner", "repo", "alice")
	require.NoError(t, err)
	assert.Equal(t, "write", got)
}

func TestClient_GenerateInstallationToken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/app/installations/67890/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]string{"token": "test-installation-token"})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	auth, err := NewAppAuth(12345, generateTestPrivateKeyPEM(t))
	require.NoError(t, err)
	client := auth.InstallationClient(67890)
	client.itr.BaseURL = server.URL

	token, err := client.GenerateInstallationToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "test-installation-token", token)
}
