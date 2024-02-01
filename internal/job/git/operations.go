package git

import (
	"os"

	"github.com/charmbracelet/log"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

func Clone(dir, cloneURL, baseRef, token string) error {
	auth := &http.BasicAuth{
		Username: "token",
		Password: token,
	}

	cloneOpts := &git.CloneOptions{
		Auth:          auth,
		SingleBranch:  true,
		URL:           cloneURL,
		ReferenceName: plumbing.NewBranchReferenceName(baseRef),
		Depth:         1,
		Progress:      os.Stdout,
	}

	repo, err := git.PlainClone(dir, false, cloneOpts)
	if err != nil {
		log.Error("error cloning", "error", err)
		return err
	}
	log.Debug("cloned", "repo", repo)

	return nil
}
