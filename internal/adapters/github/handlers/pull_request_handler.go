package handlers

import (
	"bytes"
	"fmt"
	"path/filepath"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/bmatcuk/doublestar"
	"github.com/charmbracelet/log"
	"gopkg.in/yaml.v3"

	"github.com/ivanvc/turnip/internal/adapters/github/objects"
	"github.com/ivanvc/turnip/internal/common"
	"github.com/ivanvc/turnip/internal/plugin"
	yamlconfig "github.com/ivanvc/turnip/internal/yaml"
)

func HandlePullRequest(common *common.Common, payload *objects.PullRequestWebhook) error {
	if payload.Action != "created" && payload.Action != "synchronize" {
		return nil
	}

	pr := &payload.PullRequest

	projects, err := getListOfProjectsToPlan(common, pr)
	if err != nil {
		return err
	}

	for _, prj := range projects {
		p := plugin.Load(prj.Type)
		checkURL, err := common.GitHubClient.CreateCheckRun(
			pr,
			fmt.Sprintf("turnip/%s: %s/%s", p.PlanName(), prj.Dir, p.Workspace(prj)),
		)
		if err != nil {
			log.Error("error creating check run", "error", err)
			return err
		}

		log.Debug("creating job", "checkURL", checkURL)
		repo := pr.Base.Repository
		if err := common.KubernetesClient.CreateJob("plan", repo.CloneURL, pr.Base.Ref, repo.FullName, checkURL, prj); err != nil {
			log.Error("error creating job", "error", err)
			return err
		}
	}

	return nil
}

func getListOfProjectsToPlan(common *common.Common, pr *objects.PullRequest) ([]*yamlconfig.Project, error) {
	yml, err := common.GitHubClient.FetchFile("turnip.yaml", pr.Base.Repository, pr.Base)
	output := make([]*yamlconfig.Project, 0)
	if err != nil {
		log.Error("error fetching turnip.yaml", "error", err)
		return output, err
	}

	var cfg *yamlconfig.Config
	if err := yaml.Unmarshal(yml, &cfg); err != nil {
		log.Error("error parsing configuration", "error", err)
		return output, err
	}

	diff, err := common.GitHubClient.GetPullRequestDiff(pr)
	if err != nil {
		log.Error("error getting diff", "error", err)
		return output, err
	}

	b := bytes.NewReader(diff)
	changes, _, err := gitdiff.Parse(b)
	if err != nil {
		log.Error("error parsing diff", "error", err)
		return output, err
	}

	projectRules := make(map[*yamlconfig.Project][]string)
	for _, prj := range cfg.Projects {
		ap := plugin.Load(prj.Type).AutoPlan(&prj)
		if ap == nil || ap.Disabled {
			continue
		}
		var dirs []string
		if len(ap.WhenModified) == 0 {
			dirs = []string{filepath.Join(prj.Dir, "**/*")}
		} else {
			dirs = ap.WhenModified
		}
		projectRules[&prj] = dirs
	}

	projectsTriggered := make(map[*yamlconfig.Project]bool)
	for _, change := range changes {
		var changedPath string
		if change.IsDelete {
			changedPath = change.OldName
		} else {
			changedPath = change.NewName
		}
		for prj, rules := range projectRules {
			relativePath, err := filepath.Rel(prj.Dir, changedPath)
			if err != nil {
				log.Error("error getting relative path", "error", err)
				continue
			}
			for _, rule := range rules {
				if ok, _ := doublestar.Match(rule, relativePath); ok {
					projectsTriggered[prj] = true
				}
			}
		}
	}

	for prj := range projectsTriggered {
		output = append(output, prj)
	}

	return output, nil
}

/*
func shouldCreatePlanJobUsingGit(common *common.Common, pr *objects.PullRequest) (bool, error) {
	cloneOpts := &git.CloneOptions{
		Auth:          auth,
		SingleBranch:  true,
		URL:           pr.Base.Repository.CloneURL,
		ReferenceName: plumbing.NewBranchReferenceName(pr.Base.Ref),
		Depth:         1,
		Progress:      os.Stdout,
	}

	tmpDir, err := os.MkdirTemp("", "turnip-repo-*")
	if err != nil {
		log.Error("error creating temp dir", "error", err)
		return false, err
	}
	defer os.RemoveAll(tmpDir)

	repo, err := git.PlainClone(tmpDir, false, cloneOpts)
	if err != nil {
		log.Error("error cloning", "error", err)
		return false, err
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "turnip.yaml"))
	if err != nil {
		log.Error("error reading configuration", "error", err)
		return false, err
	}

	var cfg *yamlconfig.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Error("error parsing configuration", "error", err)
		return false, err
	}

	headBranchRef := plumbing.NewBranchReferenceName(pr.Head.Ref)
	if err = repo.Fetch(&git.FetchOptions{
		Auth:     auth,
		Depth:    1,
		RefSpecs: []config.RefSpec{config.RefSpec(fmt.Sprintf("%s:%s", pr.Head.Ref, headBranchRef))},
		Progress: os.Stdout,
	}); err != nil {
		log.Error("error fetching", "error", err)
		return false, err
	}

	ref, err := repo.Head()
	if err != nil {
		log.Error("could not get head", "error", err)
		return false, err
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		log.Error("could not get commit", "error", err)
		return false, err
	}

	headRef, err := repo.ResolveRevision(plumbing.Revision(pr.Head.SHA))
	if err != nil {
		log.Error("could not resolve revision", "error", err)
		return false, err
	}
	headCommit, err := repo.CommitObject(*headRef)
	if err != nil {
		log.Error("could not get head commit", "error", err)
		return false, err
	}

	patch, err := commit.Patch(headCommit)
	if err != nil {
		log.Error("error getting patch", "error", err)
		return false, err
	}

	for _, fp := range patch.FilePatches() {
		from, _ := fp.Files()
		if from == nil {
			log.Debug("skipping deleted file", "file", from)
			continue
		}
		dir := filepath.Dir(from.Path())
		for _, prj := range cfg.Projects {
			if strings.HasPrefix(dir, prj.Dir) && prj.AutoPlan {
				return true, nil
			}
		}
	}

	return false, nil
}*/