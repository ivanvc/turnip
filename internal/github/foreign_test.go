package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// IsForeign decides whether turnip will execute a pull request's code at
// all, so each case is spelled out rather than folded into the paths that
// call it.
func TestPullRequest_IsForeign(t *testing.T) {
	base := Repository{Owner: "acme", Name: "infra"}

	tests := []struct {
		name string
		head Repository
		want bool
	}{
		{
			name: "same repository",
			head: Repository{Owner: "acme", Name: "infra"},
			want: false,
		},
		{
			// The URL differs in form for the same repository often
			// enough that comparing it would produce false refusals;
			// owner and name are what GitHub canonicalises.
			name: "same repository, different URL form",
			head: Repository{Owner: "acme", Name: "infra", URL: "git@github.com:acme/infra.git"},
			want: false,
		},
		{
			name: "fork under another owner",
			head: Repository{Owner: "contributor", Name: "infra"},
			want: true,
		},
		{
			// Same owner, different repository — a pull request across
			// two repositories one account owns is still foreign code.
			name: "same owner, different repository",
			head: Repository{Owner: "acme", Name: "other"},
			want: true,
		},
		{
			name: "head repository deleted",
			head: Repository{},
			want: true,
		},
		{
			name: "owner present, name missing",
			head: Repository{Owner: "acme"},
			want: true,
		},
		{
			name: "name present, owner missing",
			head: Repository{Name: "infra"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := &PullRequest{Number: 1, HeadRepo: tt.head}
			assert.Equal(t, tt.want, pr.IsForeign(base))
		})
	}
}

// A zero base is not a licence to run anything. It should not occur —
// every event carries a repository — but if it ever did, failing closed is
// the only safe direction.
func TestPullRequest_IsForeign_ZeroBaseStillRefuses(t *testing.T) {
	pr := &PullRequest{HeadRepo: Repository{}}
	assert.True(t, pr.IsForeign(Repository{}),
		"an unknown head repository is foreign whatever base holds")
}
