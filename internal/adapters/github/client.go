package github

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/charmbracelet/log"

	"github.com/ivanvc/turnip/internal/adapters/github/objects"
	"github.com/ivanvc/turnip/internal/config"
)

type statusRequest struct {
	State       string `json:"state,omitempty"`
	Description string `json:"description,omitempty"`
	Context     string `json:"context,omitempty"`
}

type checkRunRequest struct {
	Name        string `json:"name,omitempty"`
	HeadSHA     string `json:"head_sha,omitempty"`
	Status      string `json:"status,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	Conclusion  string `json:"conclusion,omitempty"`
}

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
	u, err := c.parseURL(strings.Replace(pr.Base.Repository.StatusesURL, "{sha}", pr.Head.SHA, 1))
	if err != nil {
		log.Error("Error parsing URL", "error", err)
		return "", err
	}

	req := statusRequest{
		State:       "pending",
		Description: "Queued",
		Context:     name,
	}

	jsonValue, err := json.Marshal(req)
	if err != nil {
		log.Error("Error marshalling", "error", err, "object", req)
		return "", err
	}
	log.Debug("creating check run", "url", u.String(), "json", string(jsonValue))
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

func (c *Client) StartCheckRun(checkURL, checkName string) error {
	u, err := c.parseURL(checkURL)
	if err != nil {
		log.Error("Error parsing URL", "error", err)
		return err
	}

	req := statusRequest{
		State:       "pending",
		Description: "Turnip is running",
		Context:     checkName,
	}

	jsonValue, _ := json.Marshal(req)
	_, err = http.Post(u.String(), "application/json", bytes.NewBuffer(jsonValue))
	if err != nil {
		log.Error("Error updating check", "error", err)
	}
	return err
}

func (c *Client) FinishCheckRun(checkURL, checkName, conclusion string) error {
	u, err := c.parseURL(checkURL)
	if err != nil {
		log.Error("Error parsing URL", "error", err)
		return err
	}

	req := statusRequest{
		State:       conclusion,
		Context:     checkName,
		Description: "Turnip has finished running",
	}

	jsonValue, _ := json.Marshal(req)
	_, err = http.Post(u.String(), "application/json", bytes.NewBuffer(jsonValue))
	if err != nil {
		log.Error("Error updating check", "error", err)
	}
	return err
}

func (c *Client) ReactToComment(reactionsURL, reaction string) error {
	u, err := c.parseURL(reactionsURL)
	if err != nil {
		log.Error("Error parsing URL", "error", err)
		return err
	}

	payload := struct {
		Content string `json:"content"`
	}{reaction}
	jsonValue, _ := json.Marshal(payload)
	resp, err := http.Post(u.String(), "application/json", bytes.NewBuffer(jsonValue))
	log.Debug("Reacted to comment", "response", resp)

	if err != nil {
		log.Error("Error reacting to comment", "error", err)
	}

	return err
}

func (c *Client) CreateComment(commentsURL, body string) error {
	u, err := c.parseURL(commentsURL)
	if err != nil {
		log.Error("Error parsing URL", "error", err)
		return err
	}

	req := struct {
		Body string `json:"body"`
	}{body}

	jsonValue, _ := json.Marshal(req)
	_, err = http.Post(u.String(), "application/json", bytes.NewBuffer(jsonValue))

	if err != nil {
		log.Error("Error creating comment", "error", err)
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

	log.Debug("fetching file", "url", u.String(), "path", path, "ref", ref)
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
	u, err := c.parseURL(pr.URL)
	if err != nil {
		log.Error("error parsing URL", "error", err)
		return nil, err
	}
	log.Debug("pr diff url", "url", u.String())

	client := &http.Client{}
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		log.Error("error creating request", "error", err)
		return nil, err
	}

	req.Header.Add("Accept", "application/vnd.github.v3.diff")
	resp, err := client.Do(req)
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
