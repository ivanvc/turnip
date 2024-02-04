package handlers

import (
	"strings"

	"github.com/charmbracelet/log"

	"github.com/ivanvc/turnip/internal/adapters/github/objects"
	"github.com/ivanvc/turnip/internal/common"
)

func HandleIssueComment(common *common.Common, issueComment *objects.IssueComment) error {
	if issueComment.Action != "created" {
		return nil
	}
	if issueComment.PullRequest == nil {
		return nil
	}
	if !strings.HasPrefix(issueComment.Comment.Body, "/turnip") {
		return nil
	}

	var err error
	issueComment.PullRequest, err = common.GitHubClient.GetPullRequestFromIssueComment(issueComment)
	if err != nil {
		log.Error("Error fetching Pull Request", "error", err)
		return err
	}

	// TODO: Implement as a cobra command
	args := strings.Fields(issueComment.Comment.Body)
	switch args[1] {
	case "plan", "preview", "diff", "pre":
		//name := fmt.Sprintf("turnip/%s: %s/%s", p.PlanName(), prj.Dir, p.Workspace(prj))
		name := "plan"
		common.GitHubClient.ReactToCommentWithThumbsUp(issueComment.Comment.Reactions.URL)
		checkURL, err := common.GitHubClient.CreateCheckRun(issueComment.PullRequest, "plan")
		if err != nil {
			log.Error("Error creating check run", "error", err)
			return err
		}
		repo := issueComment.Repository
		if err := common.KubernetesClient.CreateJob("plan", repo.CloneURL, "", repo.FullName, checkURL, name, issueComment.PullRequest.CommentsURL, nil); err != nil {
			log.Error("Error creating job", "error", err)
		}
	}

	return nil
}
