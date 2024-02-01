package github

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/log"

	"github.com/ivanvc/turnip/internal/adapters/github/objects"
	"github.com/ivanvc/turnip/internal/config"
)

type Client struct {
	token string
}

func NewClient(cfg *config.Config) *Client {
	return &Client{cfg.GitHubToken}
}

func (c *Client) GetPullRequestFromIssueComment(ic *objects.IssueComment) (*objects.PullRequest, error) {
	u, err := c.parseURL(ic.Issue.PullRequest.URL)
	if err != nil {
		log.Error("Error parsing URL", "error", err)
		return nil, err
	}

	resp, err := http.Get(u.String())
	if err != nil {
		log.Error("Error fetching Pull Request", "issueComment", ic, "error", err)
		return nil, err
	}

	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)
	var pr objects.PullRequest
	if err := decoder.Decode(&pr); err != nil {
		log.Error("Error unmarshalling", "error", err)
		return nil, err
	}

	return &pr, nil
}

func (c *Client) CreateCheckRun(pr *objects.PullRequest, name string) (string, error) {
	u, err := c.parseURL(fmt.Sprintf("%s/check-runs", pr.Base.Repository.URL))
	if err != nil {
		log.Error("Error parsing URL", "error", err)
		return "", err
	}

	req := checkRunRequest{
		Name:    fmt.Sprintf("turnip-%s", name),
		HeadSHA: pr.Head.SHA,
	}

	jsonValue, _ := json.Marshal(req)
	resp, err := http.Post(u.String(), "application/json", bytes.NewBuffer(jsonValue))
	if err != nil {
		log.Error("Error creating check", "error", err)
		return "", err
	}

	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)
	var result struct {
		URL string `json:"url"`
	}
	if err := decoder.Decode(&result); err != nil {
		log.Error("Error unmarshalling", "error", err)
		return "", err
	}
	return result.URL, nil
}

func (c *Client) StartCheckRun(checkURL string) error {
	u, err := c.parseURL(checkURL)
	if err != nil {
		log.Error("Error parsing URL", "error", err)
		return err
	}

	req := checkRunRequest{
		Status:    "in_progress",
		StartedAt: time.Now().Format(time.RFC3339),
	}

	jsonValue, _ := json.Marshal(req)
	_, err = http.Post(u.String(), "application/json", bytes.NewBuffer(jsonValue))
	if err != nil {
		log.Error("Error updating check", "error", err)
	}
	return err
}

type checkRunRequest struct {
	Name      string `json:"name"`
	HeadSHA   string `json:"head_sha"`
	Status    string `json:"status",omitempty`
	StartedAt string `json:"started_at",omitempty`
}

func (c *Client) ReactToCommentWithThumbsUp(reactionsURL string) error {
	u, err := c.parseURL(reactionsURL)
	if err != nil {
		log.Error("Error parsing URL", "error", err)
		return err
	}

	req := struct {
		Content string `json:"content"`
	}{"+1"}

	jsonValue, _ := json.Marshal(req)
	_, err = http.Post(u.String(), "application/json", bytes.NewBuffer(jsonValue))

	if err != nil {
		log.Error("Error reacting to comment", "error", err)
	}

	return err
}

func (c *Client) FetchFile(path string, repo objects.Repository, ref objects.BranchRef) ([]byte, error) {
	u, err := c.parseURL(strings.Replace(repo.ContentsURL, "{+path}", path, 1))
	if err != nil {
		log.Error("error parsing URL", "error", err)
		return nil, err
	}

	q := u.Query()
	q.Set("ref", ref.Ref)
	u.RawQuery = q.Encode()

	resp, err := http.Get(u.String())
	if err != nil {
		log.Error("error fetching file", "error", err)
		return nil, err
	}

	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)
	var result struct {
		Content string `json:"content"`
	}
	if err := decoder.Decode(&result); err != nil {
		log.Error("error unmarshalling", "error", err)
		return nil, err
	}

	data, err := base64.StdEncoding.DecodeString(result.Content)
	if err != nil {
		log.Error("error decoding bae64", "error", err)
		return nil, err
	}

	return data, nil
}

func (c *Client) GetPullRequestDiff(pr *objects.PullRequest) ([]byte, error) {
	u, err := c.parseURL(pr.DiffURL)
	if err != nil {
		log.Error("error parsing URL", "error", err)
		return nil, err
	}

	resp, err := http.Get(u.String())
	if err != nil {
		log.Error("error fetching diff", "error", err)
		return nil, err
	}

	defer resp.Body.Close()
	// Read body using io.ReadAll
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("error reading diff", "error", err)
		return nil, err
	}

	return data, nil
}

func (c *Client) parseURL(input string) (*url.URL, error) {
	u, err := url.Parse(input)
	if err != nil {
		return nil, err
	}
	u.User = url.UserPassword("token", c.token)
	return u, nil
}
