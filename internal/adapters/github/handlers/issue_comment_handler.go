package handlers

import (
	"bytes"
	"errors"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/ivanvc/turnip/internal/adapters/github/objects"
	"github.com/ivanvc/turnip/internal/common"
	"github.com/ivanvc/turnip/internal/yaml"
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

	cmd := rootCmd(common, issueComment)
	var out bytes.Buffer
	var in bytes.Reader
	cmd.SetIn(&in)
	cmd.SetOut(&out)
	cmd.SetArgs(strings.Fields(issueComment.Comment.Body)[1:])
	if err := cmd.Execute(); err != nil {
		log.Error("Error executing command", "error", err)
		common.GitHubClient.ReactToComment(issueComment.Comment.Reactions.URL, ":confused:")
		common.GitHubClient.CreateComment(issueComment.PullRequest.CommentsURL, "Error executing command: "+err.Error())
		return err
	}

	common.GitHubClient.ReactToComment(issueComment.Comment.Reactions.URL, ":+1:")

	if out.Len() > 0 {
		common.GitHubClient.CreateComment(issueComment.PullRequest.CommentsURL, out.String())
	}

	// TODO: Implement as a cobra command
	args := strings.Fields(issueComment.Comment.Body)
	switch args[1] {
	case "plan", "preview", "diff", "pre":
		//name := fmt.Sprintf("turnip/%s: %s/%s", p.PlanName(), prj.Dir, p.Workspace(prj))
		name := "plan"
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

func rootCmd(common *common.Common, ic *objects.IssueComment) *cobra.Command {
	root := &cobra.Command{
		Use:   "/turnip",
		Short: "Turnip is an IaC automation bot",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Usage()
		},
	}

	var directory, workspace, environment, stack string
	var planCmd = &cobra.Command{
		Use:     "plan",
		Aliases: []string{"preview", "diff", "pre"},
		Short:   "Plan your infrastructure",
		RunE: func(cmd *cobra.Command, args []string) error {
			if ic.PullRequest == nil {
				return errors.New("I can only plan pull requests")
			}
			yml, err := common.GitHubClient.FetchFile("turnip.yaml", ic.Repository, ic.PullRequest.Head)

			if err != nil {
				log.Error("error fetching turnip.yaml", "error", err)
				return err
			}
			log.Debug("fetched turnip.yaml", "content", string(yml))

			cfg, err := yaml.Load(yml)
			if err != nil {
				log.Error("error parsing configuration", "error", err)
				return err
			}
			log.Debug("yaml configuration", "cfg", cfg)

			// projectsToPlan := make(map[yaml.Project]bool)

			// TODO: Call function from PR Handler

			checkURL, err := common.GitHubClient.CreateCheckRun(ic.PullRequest, "plan")
			if err != nil {
				log.Error("Error creating check run", "error", err)
				return err
			}
			repo := ic.Repository
			name := "plan"
			if err := common.KubernetesClient.CreateJob("plan", repo.CloneURL, "", repo.FullName, checkURL, name, ic.PullRequest.CommentsURL, nil); err != nil {
				log.Error("Error creating job", "error", err)
			}

			return err
		},
	}
	planCmd.Flags().StringVarP(&directory, "directory", "d", directory, "the directory containing the IaC")
	// TODO: Get these from the plugins
	planCmd.Flags().StringVarP(&workspace, "workspace", "w", workspace, "the Terraform workspace to use")
	planCmd.Flags().StringVarP(&environment, "environment", "e", environment, "the Helmfile environment to use")
	planCmd.Flags().StringVarP(&stack, "stack", "s", stack, "the Pulumi stack to use")
	planCmd.MarkFlagsMutuallyExclusive("workspace", "environment", "stack")

	root.AddCommand(planCmd)
	return root
}
