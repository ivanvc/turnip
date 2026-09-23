package github

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
		content := base64.StdEncoding.EncodeToString([]byte("schemaVersion: v1alpha2\n"))
		writeJSON(t, w, map[string]string{"type": "file", "encoding": "base64", "content": content})
	})

	got, err := client.GetFile(t.Context(), "owner", "repo", "turnip.yaml", "main")
	require.NoError(t, err)
	assert.Equal(t, "schemaVersion: v1alpha2\n", string(got))
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
			"head": map[string]any{
				"sha": "abc123", "ref": "feature",
				"repo": map[string]any{
					"name":     "repo",
					"owner":    map[string]string{"login": "owner"},
					"html_url": "https://github.com/owner/repo",
				},
			},
			"base": map[string]string{"ref": "main"},
		})
	})

	got, err := client.GetPullRequest(t.Context(), "owner", "repo", 7)
	require.NoError(t, err)
	assert.Equal(t, &PullRequest{
		Number: 7, HeadSHA: "abc123", BaseRef: "main", HeadRef: "feature",
		HeadRepo: Repository{Owner: "owner", Name: "repo", URL: "https://github.com/owner/repo"},
	}, got)
}

// GetPullRequest is the *only* source of head-repository identity on the
// issue_comment path — that payload carries a pull request number and
// nothing else — so a fork's identity surviving this mapping is what lets
// a Trigger Command be refused at all.
func TestClient_GetPullRequest_CarriesForkHeadRepository(t *testing.T) {
	client, mux := newTestClient(t)

	mux.HandleFunc("/repos/owner/repo/pulls/9", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"number": 9,
			"head": map[string]any{
				"sha": "def456", "ref": "patch",
				"repo": map[string]any{
					"name":  "repo",
					"owner": map[string]string{"login": "contributor"},
				},
			},
			"base": map[string]string{"ref": "main"},
		})
	})

	got, err := client.GetPullRequest(t.Context(), "owner", "repo", 9)
	require.NoError(t, err)
	assert.Equal(t, "contributor", got.HeadRepo.Owner)
	assert.True(t, got.IsForeign(Repository{Owner: "owner", Name: "repo"}))
}

// A fork deleted after its pull request was opened leaves head.repo null.
// The mapping must survive it, and the result must be foreign — failing
// closed, since a payload turnip cannot read is not one it should run.
func TestClient_GetPullRequest_AbsentHeadRepositoryIsForeign(t *testing.T) {
	client, mux := newTestClient(t)

	mux.HandleFunc("/repos/owner/repo/pulls/11", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"number": 11,
			"head":   map[string]string{"sha": "aaa", "ref": "gone"},
			"base":   map[string]string{"ref": "main"},
		})
	})

	got, err := client.GetPullRequest(t.Context(), "owner", "repo", 11)
	require.NoError(t, err)
	assert.Equal(t, Repository{}, got.HeadRepo)
	assert.True(t, got.IsForeign(Repository{Owner: "owner", Name: "repo"}))
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

// tokenMintServer stands in for GitHub's installation-token endpoint and
// records the body it was asked with, which is where the scoping lives.
func tokenMintServer(t *testing.T) (*Client, *map[string]any) {
	t.Helper()
	body := map[string]any{}

	mux := http.NewServeMux()
	mux.HandleFunc("/app/installations/67890/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		// This endpoint is authenticated as the App, with a JWT — an
		// installation token cannot mint another one. Asserted here
		// because both transports produce a request this stub would
		// otherwise answer identically, so nothing else would notice the
		// wrong one being used until GitHub returned 401 in production.
		assert.True(t, isAppJWT(r.Header.Get("Authorization")),
			"the mint must authenticate as the App, not as the installation: %q", r.Header.Get("Authorization"))
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		writeJSON(t, w, map[string]string{"token": "test-installation-token"})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	auth, err := NewAppAuth(12345, generateTestPrivateKeyPEM(t))
	require.NoError(t, err)
	client := auth.InstallationClient(67890)
	client.itr.BaseURL = server.URL

	return client, &body
}

func TestClient_GenerateInstallationToken(t *testing.T) {
	client, _ := tokenMintServer(t)

	token, err := client.GenerateInstallationToken(t.Context(), TokenScope{Repositories: []string{"web"}})
	require.NoError(t, err)
	assert.Equal(t, "test-installation-token", token.Token)
}

// The narrowing is the point of the call, so it is asserted on the wire
// rather than on the struct that was passed in.
func TestClient_GenerateInstallationToken_NarrowsRepositoriesAndPermissions(t *testing.T) {
	client, body := tokenMintServer(t)

	_, err := client.GenerateInstallationToken(t.Context(), TokenScope{Repositories: []string{"web", "shared-modules"}})
	require.NoError(t, err)

	assert.Equal(t, []any{"web", "shared-modules"}, (*body)["repositories"])
	assert.Equal(t, map[string]any{"contents": "read"}, (*body)["permissions"],
		"the clone needs contents:read and nothing else")
}

// An empty scope is how a recursive submodule clone is permitted to reach
// the whole installation (Requirement 1.4). Permissions are narrowed even
// then — that half is never unknowable.
func TestClient_GenerateInstallationToken_EmptyScopeStillNarrowsPermissions(t *testing.T) {
	client, body := tokenMintServer(t)

	_, err := client.GenerateInstallationToken(t.Context(), TokenScope{})
	require.NoError(t, err)

	assert.NotContains(t, *body, "repositories", "omitted, not empty — an empty list would scope to nothing")
	assert.Equal(t, map[string]any{"contents": "read"}, (*body)["permissions"])
}

// Minting must not narrow the Client's own transport: the Server posts
// comments and check runs through it, and neither survives contents:read.
// This is the bug that would look like "dispatch works, reporting fails".
func TestClient_GenerateInstallationToken_LeavesTheAPITransportUnscoped(t *testing.T) {
	client, _ := tokenMintServer(t)

	_, err := client.GenerateInstallationToken(t.Context(), TokenScope{Repositories: []string{"web"}})
	require.NoError(t, err)

	assert.Nil(t, client.itr.InstallationTokenOptions,
		"the transport backing comments and check runs must stay unscoped")
}

// GetPullRequest is the only source of pull-request state on the
// issue_comment path, exactly as it is for the head repository.
func TestClient_GetPullRequest_CarriesOpenState(t *testing.T) {
	for _, tc := range []struct {
		name string
		body map[string]any
		want bool
	}{
		{"open", map[string]any{"number": 9, "state": "open"}, true},
		{"closed without merging", map[string]any{"number": 9, "state": "closed", "merged": false}, false},
		{"merged", map[string]any{"number": 9, "state": "closed", "merged": true}, false},
		{"state absent", map[string]any{"number": 9}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, mux := newTestClient(t)
			body := tc.body
			body["head"] = map[string]any{"sha": "def456", "ref": "patch"}
			body["base"] = map[string]string{"ref": "main"}

			mux.HandleFunc("/repos/owner/repo/pulls/9", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, body)
			})

			got, err := client.GetPullRequest(t.Context(), "owner", "repo", 9)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Open)
		})
	}
}

// isAppJWT reports whether an Authorization header carries a JWT signed
// as the App, rather than an opaque installation token.
func isAppJWT(header string) bool {
	raw, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return false
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return false
	}
	head, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	var typ struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(head, &typ); err != nil {
		return false
	}
	return typ.Typ == "JWT" && typ.Alg != ""
}
