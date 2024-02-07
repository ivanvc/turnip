package handlers

import (
	"bytes"
	"fmt"
	"path/filepath"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/bmatcuk/doublestar"
	"github.com/charmbracelet/log"

	"github.com/ivanvc/turnip/internal/adapters/github/objects"
	"github.com/ivanvc/turnip/internal/common"
	"github.com/ivanvc/turnip/internal/plugin"
	"github.com/ivanvc/turnip/internal/yaml"
)

func HandlePullRequest(common *common.Common, payload *objects.PullRequestWebhook) error {
	if payload.Action != "opened" && payload.Action != "synchronize" {
		return nil
	}

	pr := &payload.PullRequest

	projects, err := getListOfProjectsToPlan(common, pr)
	if err != nil {
		return err
	}

	return triggerProjects(common, pr, projects)
}

func triggerProjects(common *common.Common, pr *objects.PullRequest, projects []*yaml.Project) error {
	for _, prj := range projects {
		p := plugin.Load(prj.LoadedWorkflow.Type)
		name := fmt.Sprintf("turnip/%s: %s/%s", p.PlanName(), prj.Dir, p.Workspace(prj))
		checkURL, err := common.GitHubClient.CreateCheckRun(pr, name)
		if err != nil {
			log.Error("error creating check run", "error", err)
			return err
		}

		log.Debug("creating job", "checkURL", checkURL)
		repo := pr.Base.Repository
		if err := common.KubernetesClient.CreateJob("plan", repo.CloneURL, pr.Head.Ref, repo.FullName, checkURL, name, pr.CommentsURL, prj); err != nil {
			log.Error("error creating job", "error", err)
			return err
		}
	}

	return nil
}

func getListOfProjectsToPlan(common *common.Common, pr *objects.PullRequest) ([]*yaml.Project, error) {
	yml, err := common.GitHubClient.FetchFile("turnip.yaml", pr.Head.Repository, pr.Head)
	output := make([]*yaml.Project, 0)
	if err != nil {
		log.Error("error fetching turnip.yaml", "error", err)
		return output, err
	}
	log.Debug("fetched turnip.yaml", "content", string(yml))

	cfg, err := yaml.Load(yml)
	if err != nil {
		log.Error("error parsing configuration", "error", err)
		return output, err
	}
	log.Debug("yaml configuration", "cfg", cfg)

	diff, err := common.GitHubClient.GetPullRequestDiff(pr)
	if err != nil {
		log.Error("error getting diff", "error", err)
		return output, err
	}
	log.Debug("pr diff", "diff", string(diff))

	b := bytes.NewReader(diff)
	changes, _, err := gitdiff.Parse(b)
	if err != nil {
		log.Error("error parsing diff", "error", err)
		return output, err
	}

	projectRules := make(map[*yaml.Project][]string)
	for _, prj := range cfg.Projects {
		log.Debug("checking project", "project", prj)
		if !plugin.Load(prj.LoadedWorkflow.Type).AutoPlan(&prj) {
			continue
		}
		var dirs []string
		if len(prj.WhenModified) == 0 {
			dirs = []string{"**/*"}
		} else {
			dirs = prj.WhenModified
		}
		projectRules[&prj] = dirs
	}
	log.Debug("project rules", "rules", projectRules)

	projectsTriggered := make(map[*yaml.Project]bool)
	for _, change := range changes {
		var changedPath string
		if change.IsDelete {
			changedPath = change.OldName
		} else {
			changedPath = change.NewName
		}
		log.Debug("checking changed path", "path", changedPath)
		for prj, rules := range projectRules {
			relativePath, err := filepath.Rel(prj.Dir, changedPath)
			if err != nil {
				log.Error("error getting relative path", "error", err)
				continue
			}
			log.Debug("checking relative path", "path", relativePath)
			for _, rule := range rules {
				log.Debug("checking rule", "rule", rule)
				if ok, _ := doublestar.Match(rule, relativePath); ok {
					log.Debug("rule matched!")
					projectsTriggered[prj] = true
				}
			}
		}
	}

	for prj := range projectsTriggered {
		output = append(output, prj)
	}
	log.Debug("projects to plan", "projects", output)

	return output, nil
}
