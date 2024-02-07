package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
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
	// TODO: Support running commands without the /turnip prefix, i.e. /plan, /apply.
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
	cmd.SetErr(&out)
	cmd.SetArgs(strings.Fields(issueComment.Comment.Body)[1:])
	if err := cmd.Execute(); err != nil {
		log.Error("Error executing command", "error", err)
		if err := common.GitHubClient.ReactToComment(issueComment.Comment.Reactions.URL, "confused"); err != nil {
			log.Error("Error reacting to comment", "error", err)
		}
		if err := common.GitHubClient.CreateComment(issueComment.PullRequest.CommentsURL, "Error executing command: "+err.Error()); err != nil {
			log.Error("Error creating comment", "error", err)
		}
		return err
	}

	if err := common.GitHubClient.ReactToComment(issueComment.Comment.Reactions.URL, "+1"); err != nil {
		log.Error("Error reacting to comment", "error", err)
	}

	if out.Len() > 0 {
		if err := common.GitHubClient.CreateComment(issueComment.PullRequest.CommentsURL, fmt.Sprintf("```\n%s\n```", out.String())); err != nil {
			log.Error("Error creating comment", "error", err)
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
		Aliases: []string{"preview", "pre", "diff"},
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

			projects, err := getListOfProjectsToPlan(common, ic.PullRequest)
			if err != nil {
				log.Error("Error getting list of projects to plan", "error", err)
				return err
			}
			log.Debug("projects to plan", "projects", projects)

			if len(directory) > 0 {
				projects = slices.DeleteFunc(projects, func(p *yaml.Project) bool {
					return p.Dir == directory
				})
			}
			log.Debug("projects to plan after directory filter", "projects", projects)

			if len(workspace) > 0 {
				projects = slices.DeleteFunc(projects, func(p *yaml.Project) bool {
					return p.Workspace == workspace
				})
			}
			log.Debug("projects to plan after workspace filter", "projects", projects)

			if len(projects) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No projects to plan")
				return nil
			}

			return triggerProjects(common, ic.PullRequest, projects)
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
