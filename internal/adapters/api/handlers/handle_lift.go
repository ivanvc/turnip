package handlers

import (
	"fmt"

	"github.com/charmbracelet/log"

	"github.com/ivanvc/turnip/internal/adapters/api/objects"
	githubObjects "github.com/ivanvc/turnip/internal/adapters/github/objects"
	"github.com/ivanvc/turnip/internal/common"
	"github.com/ivanvc/turnip/internal/yaml"
)

func HandleLift(common *common.Common, payload *objects.LiftRequest) (*objects.LiftResponse, error) {
	prj, err := getProject(common, payload)
	if err != nil {
		return nil, err
	}

	return triggerProject(common, "lift", prj, payload)
}

func getProject(common *common.Common, payload *objects.LiftRequest) (*yaml.Project, error) {
	repo := githubObjects.Repository{
		ContentsURL: fmt.Sprintf("https://api.github.com/repos/%s/contents/{+path}", payload.Repo),
	}
	ref := githubObjects.BranchRef{
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

func triggerProject(common *common.Common, cmdName string, project *yaml.Project, payload *objects.LiftRequest) (*objects.LiftResponse, error) {
	var cmd string
	switch cmdName {
	case "plot":
		cmd = project.GetPlotName()
	case "lift":
		cmd = project.GetLiftName()
	}

	name := fmt.Sprintf("turnip/%s/%s/%s/%s", project.GetAdapterName(), cmd, project.Dir, project.GetWorkspace())
	checkURL, err := common.GitHubClient.CreateCheckRun(
		fmt.Sprintf("https://api.github.com/repos/%s/statuses/{sha}", payload.Repo),
		payload.Ref,
		name,
	)
	if err != nil {
		log.Error("error creating check run", "error", err)
		return nil, err
	}

	log.Debug("creating job", "checkURL", checkURL)
	cloneURL := fmt.Sprintf("https://github.com/%s.git", payload.Repo)
	commit, err := common.GitHubClient.GetCommitFromRef(payload.Repo, payload.Ref)
	if err != nil {
		log.Error("error getting commit", "error", err)
		return nil, err
	}

	if err := common.KubernetesClient.CreateJob(cmdName, cloneURL, payload.Ref, payload.Repo, checkURL, name, commit.CommentsURL, project); err != nil {
		log.Error("error creating job", "error", err)
		return nil, err
	}

	return &objects.LiftResponse{checkURL}, nil
}
