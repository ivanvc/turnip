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
	// TODO: Support running commands without the /turnip prefix, i.e. /plot, /apply.
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
		if err := common.GitHubClient.CreateComment(issueComment.PullRequest.CommentsURL, fmt.Sprintf("Error executing command:\n\n```\n%s\n```", err.Error())); err != nil {
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
		Use:               "/turnip",
		Short:             "Turnip is an IaC automation bot",
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Usage()
		},
	}

	root.AddCommand(getCobraCmd(common, ic, "plot"))
	root.AddCommand(getCobraCmd(common, ic, "lift"))
	return root
}

func getCobraCmd(common *common.Common, ic *objects.IssueComment, cmdName string) *cobra.Command {
	var directory, workspace, environment, stack, description string
	var aliases []string

	switch cmdName {
	case "plot":
		aliases = []string{"diff", "plan", "preview", "pre"}
		description = "Plot changes in your infrastructure"
	case "lift":
		aliases = []string{"apply", "deploy", "up"}
		description = "Lift applies changes in your infrastructure"
	}

	var cmd = &cobra.Command{
		Use:     cmdName,
		Aliases: aliases,
		Short:   description,
		RunE: func(cmd *cobra.Command, args []string) error {
			if ic.PullRequest == nil {
				return errors.New("I can only work on pull requests")
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

			projects, err := getListOfProjectsToPlot(common, ic.PullRequest, false)
			if err != nil {
				log.Error("Error getting list of projects", "error", err)
				return err
			}
			log.Debug("projects to run", "projects", projects)

			if len(directory) > 0 {
				projects = slices.DeleteFunc(projects, func(p *yaml.Project) bool {
					return p.Dir == directory
				})
			}
			log.Debug("projects to run after directory filter", "projects", projects)

			if len(workspace) > 0 {
				projects = slices.DeleteFunc(projects, func(p *yaml.Project) bool {
					return p.Workspace == workspace
				})
			}
			log.Debug("projects to run after workspace filter", "projects", projects)

			if len(projects) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No projects to"+cmdName)
				return nil
			}

			return triggerProjects(common, cmdName, ic.PullRequest, projects)
		},
	}
	cmd.Flags().StringVarP(&directory, "directory", "d", directory, "the directory containing the IaC")
	// TODO: Get these from the plugins
	cmd.Flags().StringVarP(&workspace, "workspace", "w", workspace, "the Terraform workspace to use")
	cmd.Flags().StringVarP(&environment, "environment", "e", environment, "the Helmfile environment to use")
	cmd.Flags().StringVarP(&stack, "stack", "s", stack, "the Pulumi stack to use")
	cmd.MarkFlagsMutuallyExclusive("workspace", "environment", "stack")

	return cmd
}
