package github

import (
	"fmt"
	"net/http"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gh "github.com/google/go-github/v90/github"
)

// AppAuth parses a GitHub App's private key once and mints an
// installation-scoped Client for any installation of that App.
type AppAuth struct {
	appsTransport *ghinstallation.AppsTransport
}

// NewAppAuth parses appPrivateKeyPEM immediately, returning an error on a
// malformed key rather than deferring the failure to first use.
func NewAppAuth(appID int64, appPrivateKeyPEM []byte) (*AppAuth, error) {
	transport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, appID, appPrivateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("github: parsing app private key: %w", err)
	}
	return &AppAuth{appsTransport: transport}, nil
}

// InstallationClient returns a Client authenticated as the given
// installation. Cheap to call repeatedly — it shares the AppAuth's parsed
// key rather than re-parsing it.
func (a *AppAuth) InstallationClient(installationID int64) *Client {
	itr := ghinstallation.NewFromAppsTransport(a.appsTransport, installationID)

	// WithTransport only errors when given a nil transport; itr is always
	// non-nil here, so this can never fail.
	ghClient, _ := gh.NewClient(gh.WithTransport(itr))

	return &Client{gh: ghClient, itr: itr}
}
