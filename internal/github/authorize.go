package github

import (
	"context"
	"sync"
	"time"
)

const authorizationCacheTTL = 5 * time.Minute

var permissionRank = map[string]int{
	"none":     1,
	"read":     2,
	"triage":   3,
	"write":    4,
	"maintain": 5,
	"admin":    6,
}

// cacheEntry holds whichever authorization answers have been fetched for
// one account on one repository.
//
// The two stay separate and are cached separately. Collaborator status
// comes from the 204/404 endpoint, which answers "is this person a
// collaborator"; the permission level comes from the endpoint that reports
// what access an account has. A successful permission lookup on its own
// says GitHub knows the account, not that it trusts them: on a public
// repository every account reads as "read".
//
// IsCollaborator is the one place one answer informs the other, and only
// under the visibility rule in collaboratorByRole: when the collaborator
// answer leaves an account out (as it does for access granted through a
// team), a role the repository's visibility cannot explain still counts.
// Nothing else may infer collaboration from a permission level.
//
// One expiry covers both, set when the entry is created. An answer fetched
// late in the window therefore lives less than the full TTL, which errs
// toward asking GitHub again.
type cacheEntry struct {
	collaborator     bool
	haveCollaborator bool

	permission     string
	havePermission bool

	expiresAt time.Time
}

// Authorizer answers collaborator/write-permission questions against a
// GitHubClient, caching each (owner, repo, username)'s permission level
// for 5 minutes so repeated triggers from the same author don't re-hit the
// GitHub API.
type Authorizer struct {
	client GitHubClient

	mu    sync.Mutex
	cache map[string]cacheEntry

	now func() time.Time
}

func NewAuthorizer(client GitHubClient) *Authorizer {
	return &Authorizer{
		client: client,
		cache:  make(map[string]cacheEntry),
		now:    time.Now,
	}
}

// IsCollaborator reports whether username is a collaborator on
// owner/repo: listed by GitHub's collaborator answer, or holding a role
// the repository's visibility does not explain.
//
// The collaborator answer is asked first, and a yes is final, so an
// account it lists costs no permission lookup. That endpoint replies 204
// or 404, and go-github maps the 404 to a clean false, so "not listed"
// and "could not tell" stay distinguishable.
//
// On a 404 the permission answer decides, read through the same cache
// HasWritePermission uses, so a team member who goes on to ask for an
// apply is still asked about once. GitHub's collaborator answer, asked by
// an App with no organization permission, leaves out access granted
// through a team, while the permission answer reports it.
//
// An error from either lookup is returned as an error, never as a no, and
// a caller must refuse rather than read it as one: an under-permissioned
// installation, a rate limit or an outage lands here and must not look
// like a repository with no collaborators.
func (a *Authorizer) IsCollaborator(ctx context.Context, owner, repo, username string, private bool) (bool, error) {
	listed, err := a.listedCollaborator(ctx, owner, repo, username)
	if err != nil {
		return false, err
	}
	if listed {
		return true, nil
	}

	perm, err := a.permission(ctx, owner, repo, username)
	if err != nil {
		return false, err
	}
	return collaboratorByRole(perm, private), nil
}

// collaboratorByRole is the visibility rule: whether a role reported by
// the permission answer is access the repository's visibility does not
// explain. On a private repository that is any role above none; on a
// public one it is a role above read, since GitHub reports read for every
// account there. A role turnip does not recognize ranks below none and
// admits no one.
func collaboratorByRole(perm string, private bool) bool {
	floor := permissionRank["read"]
	if private {
		floor = permissionRank["none"]
	}
	return permissionRank[perm] > floor
}

// listedCollaborator asks GitHub's collaborator answer, through the cache.
func (a *Authorizer) listedCollaborator(ctx context.Context, owner, repo, username string) (bool, error) {
	if entry, ok := a.cached(owner, repo, username); ok && entry.haveCollaborator {
		return entry.collaborator, nil
	}

	isCollaborator, err := a.client.IsCollaborator(ctx, owner, repo, username)
	if err != nil {
		return false, err
	}

	a.store(owner, repo, username, func(e *cacheEntry) {
		e.collaborator, e.haveCollaborator = isCollaborator, true
	})

	return isCollaborator, nil
}

// HasWritePermission reports whether username's permission level is
// "write" or higher (write, maintain, admin).
func (a *Authorizer) HasWritePermission(ctx context.Context, owner, repo, username string) (bool, error) {
	perm, err := a.permission(ctx, owner, repo, username)
	if err != nil {
		return false, err
	}
	return permissionRank[perm] >= permissionRank["write"], nil
}

func (a *Authorizer) permission(ctx context.Context, owner, repo, username string) (string, error) {
	if entry, ok := a.cached(owner, repo, username); ok && entry.havePermission {
		return entry.permission, nil
	}

	perm, err := a.client.GetCollaboratorPermission(ctx, owner, repo, username)
	if err != nil {
		return "", err
	}

	a.store(owner, repo, username, func(e *cacheEntry) {
		e.permission, e.havePermission = perm, true
	})

	return perm, nil
}

func cacheKey(owner, repo, username string) string {
	return owner + "/" + repo + "/" + username
}

// cached returns the live entry for an account, if one has not expired.
func (a *Authorizer) cached(owner, repo, username string) (cacheEntry, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	entry, ok := a.cache[cacheKey(owner, repo, username)]
	if !ok || !a.now().Before(entry.expiresAt) {
		return cacheEntry{}, false
	}
	return entry, true
}

// store records one answer, creating the entry (and starting its lifetime)
// when there is not a live one. An expired entry is replaced rather than
// amended, so a stale answer never survives because a fresh one arrived
// beside it.
func (a *Authorizer) store(owner, repo, username string, record func(*cacheEntry)) {
	a.mu.Lock()
	defer a.mu.Unlock()

	key := cacheKey(owner, repo, username)
	entry, ok := a.cache[key]
	if !ok || !a.now().Before(entry.expiresAt) {
		entry = cacheEntry{expiresAt: a.now().Add(authorizationCacheTTL)}
	}
	record(&entry)
	a.cache[key] = entry
}
