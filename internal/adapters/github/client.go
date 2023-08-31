package github

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/charmbracelet/log"

	"github.com/ivanvc/turnip/internal/config"
)

type Client struct {
	token string
}

func NewClient(cfg *config.Config) *Client {
	return &Client{cfg.GitHubToken}
}

func (c *Client) GetPullRequestFromIssueComment(ic *IssueComment) *PullRequest {
	u, err := url.Parse(ic.Issue.PullRequest.URL)
	if err != nil {
		log.Error("Error parsing URL", "error", err)
		return nil
	}

	u.User = url.UserPassword("token", c.token)
	resp, err := http.Get(u.String())
	if err != nil {
		log.Error("Error fetching Pull Request", "issueComment", ic, "error", err)
		return nil
	}

	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)
	var pr PullRequest
	if err := decoder.Decode(&pr); err != nil {
		log.Error("Error unmarshalling", "error", err)
		return nil
	}

	return &pr
}
