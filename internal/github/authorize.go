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
// The two are independent and neither implies the other. Collaborator
// status comes from the 204/404 endpoint, which answers "is this person a
// collaborator"; the permission level comes from the endpoint that reports
// what access an account has. Inferring the first from the second is the
// defect this shape exists to make unexpressible — a successful permission
// lookup says GitHub knows the account, not that it trusts them.
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

// IsCollaborator reports whether GitHub considers username a collaborator
// on owner/repo.
//
// It asks the endpoint that answers that question, which replies 204 or
// 404 — go-github maps the 404 to a clean false, so "not a collaborator"
// and "could not tell" stay distinguishable. Anything else is returned as
// an error, and a caller must refuse rather than read it as a no: GitHub
// requires write, maintain or admin to read collaborator information, so
// an under-permissioned installation lands here and must not look like a
// repository with no collaborators.
//
// Deliberately not derived from a permission lookup. That endpoint reports
// what access an account has, and reporting *some* access is not evidence
// of collaboration — on a public repository read access is universal. The
// gate once inferred one from the other and admitted every account GitHub
// would answer about at all.
func (a *Authorizer) IsCollaborator(ctx context.Context, owner, repo, username string) (bool, error) {
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
