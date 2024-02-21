package handlers

import (
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/ivanvc/turnip/internal/adapters/api/objects"
	githubobjects "github.com/ivanvc/turnip/internal/adapters/github/objects"
	"github.com/ivanvc/turnip/internal/common"
	"github.com/ivanvc/turnip/internal/yaml"
)

func Handle(common *common.Common, verb string, payload *objects.APIRequest) (*objects.APIResponse, error) {
	prj, err := getProject(common, payload)
	if err != nil {
		return nil, err
	}

	return triggerProject(common, verb, prj, payload)
}

func getProject(common *common.Common, payload *objects.APIRequest) (*yaml.Project, error) {
	repo := githubobjects.Repository{
		ContentsURL: fmt.Sprintf("https://api.github.com/repos/%s/contents/{+path}", payload.Repo),
	}
	ref := githubobjects.BranchRef{
		Ref: payload.Ref,
	}

	yml, err := common.GitHubClient.FetchFile("turnip.yaml", repo, ref)
	if err != nil {
		log.Error("error fetching turnip.yaml", "error", err)
		return nil, err
	}
	log.Debug("fetched turnip.yaml", "content", string(yml))

	cfg, err := yaml.Load(yml)
	if err != nil {
		log.Error("error parsing configuration", "error", err)
		return nil, err
	}
	log.Debug("yaml configuration", "cfg", cfg)

	for _, prj := range cfg.Projects {
		if prj.Dir == payload.Dir {
			switch prj.GetWorkspace() {
			case payload.Workspace, payload.Environment, payload.Stack:
				return &prj, nil
			}
		}
	}

	return nil, fmt.Errorf("project not found")
}

func triggerProject(common *common.Common, cmdName string, project *yaml.Project, payload *objects.APIRequest) (*objects.APIResponse, error) {
	var cmd string
	switch cmdName {
	case "plot":
		cmd = project.GetPlotName()
	case "lift":
		cmd = project.GetLiftName()
	}

	commit, err := common.GitHubClient.GetCommitFromRef(payload.Repo, payload.Ref)
	if err != nil {
		log.Error("error getting commit", "error", err)
		return nil, err
	}

	name := fmt.Sprintf("turnip/%s/%s/%s/%s", project.GetAdapterName(), cmd, project.Dir, project.GetWorkspace())
	checkURL, err := common.GitHubClient.CreateCheckRun(
		fmt.Sprintf("https://api.github.com/repos/%s/statuses/{sha}", payload.Repo),
		commit.SHA,
		name,
	)
	if err != nil {
		log.Error("error creating check run", "error", err)
		return nil, err
	}

	log.Debug("creating job", "checkURL", checkURL)
	cloneURL := fmt.Sprintf("https://github.com/%s.git", payload.Repo)

	if err := common.KubernetesClient.CreateJob(cmdName, cloneURL, payload.Ref, payload.Repo, checkURL, name, commit.CommentsURL, payload.ExtraArgs, project); err != nil {
		log.Error("error creating job", "error", err)
		return nil, err
	}

	return &objects.APIResponse{CheckURL: checkURL, Context: name}, nil
}
