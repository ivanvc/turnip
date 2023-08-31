package mai

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/ivanvc/turnip/internal/adapters/github"
	"github.com/ivanvc/turnip/internal/yaml"
)

func main() {
	/*
		conn, err := grpc.Dial("turnip:50001", grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Fatalf("did not connect: %v", err)
		}
		defer conn.Close()
		c := pb.NewGreeterClient(conn)

		// Contact the server and print out its response.
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
	*/

	var p github.IssueComment
	if err := json.Unmarshal([]byte(os.Getenv("TURNIP_PAYLOAD")), &p); err != nil {
		log.Error("Error unmashalling", "error", err)
	}

	log.Info("Got Payload", "payload", p)

	opts := &git.CloneOptions{
		Auth: &http.BasicAuth{
			Username: "token",
			Password: os.Getenv("TURNIP_GITHUB_TOKEN"),
		},
		SingleBranch:  true,
		URL:           p.Repository.CloneURL,
		ReferenceName: plumbing.NewBranchReferenceName(p.Issue.PullRequest.Head.Ref),
		Depth:         1,
		Progress:      os.Stdout,
	}

	log.Info("Cloning repo", "opts", opts)

	tmp, err := os.MkdirTemp("", "turnip-repo-*")
	if err != nil {
		log.Fatal("Error creating temp dir", "error", err)
	}
	defer os.RemoveAll(tmp)

	repo, err := git.PlainClone(tmp, false, opts)
	if err != nil {
		log.Fatal("Error clonning", "error", err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, "turnip.yaml"))
	if err != nil {
		log.Fatal("Error reading configuration", "error", err)
	}

	var cfg *yaml.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatal("Error parsing configuration", "error", err)
	}
	log.Info("Configuration loaded", "config", cfg)

	baseBranchRef := plumbing.NewBranchReferenceName(p.Issue.PullRequest.Base.Ref)
	log.Info("Fetching base branch", "baseRef", baseBranchRef, "ref", config.RefSpec(fmt.Sprintf("%s:%s", p.Issue.PullRequest.Base.Ref, baseBranchRef)), "reff", p.Issue.PullRequest.Base)
	err = repo.Fetch(&git.FetchOptions{
		Auth: &http.BasicAuth{
			Username: "token",
			Password: os.Getenv("TURNIP_GITHUB_TOKEN"),
		},
		Depth:    1,
		RefSpecs: []config.RefSpec{config.RefSpec(fmt.Sprintf("%s:%s", p.Issue.PullRequest.Base.Ref, baseBranchRef))},
		Progress: os.Stdout,
	})
	if err != nil {
		log.Fatal("error fetching remote", "error", err)
	}

	log.Info("Generating diff")
	ref, err := repo.Head()
	if err != nil {
		log.Fatal("could not get head", "error", err)
	}

	log.Info("Repo HEAD", "head", ref)
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		log.Fatal("could not get commit object for ref", "error", err, "ref", ref)
	}

	log.Info("Commit", "commit", commit)

	baseRef, err := repo.ResolveRevision(plumbing.Revision(p.Issue.PullRequest.Base.SHA))
	if err != nil {
		log.Fatal("could not resolve revision", "error", err)
	}
	baseCommit, err := repo.CommitObject(*baseRef)
	if err != nil {
		log.Fatal("could not get commit object for ref", "error", err, "baseRef", baseRef, "baseBranchRef", baseBranchRef)
	}
	patch, err := commit.Patch(baseCommit)
	if err != nil {
		log.Fatal("error getting patch", "error", err)
	}
	for _, p := range patch.FilePatches() {
		from, to := p.Files()
		msg := fmt.Sprintf("%s -> %s", from.Path(), to.Path())
		//	r, err := c.SayHello(ctx, &pb.HelloRequest{Name: msg})
		//	if err != nil {
		//		log.Fatalf("could not greet: %v", err)
		//	}
		log.Infof("Greeting: %s", msg)
	}

}
