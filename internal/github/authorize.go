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

type cacheEntry struct {
	permission string
	expiresAt  time.Time
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

// IsCollaborator reports whether username has any access to owner/repo.
func (a *Authorizer) IsCollaborator(ctx context.Context, owner, repo, username string) (bool, error) {
	if _, err := a.permission(ctx, owner, repo, username); err != nil {
		return false, err
	}
	return true, nil
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
	key := owner + "/" + repo + "/" + username

	a.mu.Lock()
	entry, ok := a.cache[key]
	a.mu.Unlock()
	if ok && a.now().Before(entry.expiresAt) {
		return entry.permission, nil
	}

	perm, err := a.client.GetCollaboratorPermission(ctx, owner, repo, username)
	if err != nil {
		return "", err
	}

	a.mu.Lock()
	a.cache[key] = cacheEntry{permission: perm, expiresAt: a.now().Add(authorizationCacheTTL)}
	a.mu.Unlock()

	return perm, nil
}
