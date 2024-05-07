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
	"github.com/ivanvc/turnip/internal/yaml"
)

func HandlePullRequest(common *common.Common, payload *objects.PullRequestWebhook) error {
	// TODO: if we have built this PR before, plot (plan) the projects that have been planned before
	if payload.Action != "opened" && payload.Action != "synchronize" {
		return nil
	}

	pr := &payload.PullRequest

	projects, err := getListOfProjectsToPlot(common, pr, true)
	if err != nil {
		return err
	}

	return triggerProjects(common, "plot", "", pr, projects)
}

func triggerProjects(common *common.Common, cmdName, extraArgs string, pr *objects.PullRequest, projects []*yaml.Project) error {
	for _, prj := range projects {
		var cmd string
		switch cmdName {
		case "plot":
			cmd = prj.GetPlotName()
		case "lift":
			cmd = prj.GetLiftName()
		}

		name := fmt.Sprintf("turnip/%s/%s/%s/%s", prj.GetAdapterName(), cmd, prj.Dir, prj.GetWorkspace())
		checkURL, err := common.GitHubClient.CreateCheckRun(
			pr.Base.Repository.StatusesURL,
			pr.Head.SHA,
			name,
		)
		if err != nil {
			log.Error("error creating check run", "error", err)
			return err
		}

		log.Debug("creating job", "checkURL", checkURL)
		repo := pr.Base.Repository
		if err := common.KubernetesClient.CreateJob(cmdName, repo.CloneURL, pr.Head.Ref, repo.FullName, checkURL, name, pr.CommentsURL, extraArgs, prj); err != nil {
			log.Error("error creating job", "error", err)
			return err
		}
	}

	return nil
}

func getListOfProjectsToPlot(common *common.Common, pr *objects.PullRequest, autoPlot bool) ([]*yaml.Project, error) {
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
		log.Debug("checking project", "project", prj, "autoPlot", autoPlot, "prj.AutoPlot", prj.GetAutoPlot())
		if autoPlot && prj.GetAutoPlot() == false {
			continue
		}
		var dirs []string
		if len(prj.WhenModified) == 0 {
			dirs = []string{"./**/*"}
		} else {
			for _, rule := range prj.WhenModified {
				if rule[0:2] != ".." && rule[0:2] != "./" {
					rule = fmt.Sprintf("./%s", rule)
				}
				dirs = append(dirs, rule)
			}
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
			if ok, err := doesPathMatchProjectRules(prj.Dir, changedPath, rules); err != nil {
				log.Error("error checking path", "error", err)
				continue
			} else if ok {
				projectsTriggered[prj] = true
			}
		}
	}

	for prj := range projectsTriggered {
		output = append(output, prj)
	}
	log.Debug("projects to plot", "projects", output)

	return output, nil
}

func doesPathMatchProjectRules(dir, path string, rules []string) (bool, error) {
	relPath, err := filepath.Rel(dir, path)
	if err != nil {
		return false, err
	}
	if len(relPath) > 2 && relPath[0:2] != ".." {
		relPath = fmt.Sprintf("./%s", relPath)
	}
	for _, rule := range rules {
		log.Debug("checking rule", "value", rule)
		if ok, _ := doublestar.Match(rule, relPath); ok {
			log.Debug("rule matched", "rule", rule, "path", path)
			return true, nil
		}
	}
	return false, nil
}
