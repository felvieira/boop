package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chrisgreg/boop/server/internal/support"
)

var errInvalidGitHubToken = errors.New("GitHub rejected this token")

type githubIssueClient struct {
	http *http.Client
}

type githubIssue struct {
	ID      int64  `json:"id"`
	Number  int64  `json:"number"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}

type githubIssuePayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func newGitHubIssueClient() *githubIssueClient {
	return &githubIssueClient{http: &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Host != "api.github.com" || len(via) >= 2 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}}
}

func (c *githubIssueClient) request(ctx context.Context, token, method, endpoint string, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "Boop-support-actions")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

func (c *githubIssueClient) validateToken(ctx context.Context, token string) error {
	resp, err := c.request(ctx, token, http.MethodGet, "/user", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errInvalidGitHubToken
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GitHub token validation returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (c *githubIssueClient) findIssue(ctx context.Context, token, repo, marker string) (*githubIssue, error) {
	parts := strings.SplitN(repo, "/", 2)
	endpoint := "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/issues"
	for page := 1; page <= 5; page++ {
		query := "?state=all&per_page=100&sort=created&direction=desc&page=" + strconv.Itoa(page)
		resp, err := c.request(ctx, token, http.MethodGet, endpoint+query, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, fmt.Errorf("GitHub could not check existing issues (HTTP %d)", resp.StatusCode)
		}
		var issues []githubIssue
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&issues)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, errors.New("GitHub returned an unreadable issue list")
		}
		for i := range issues {
			if strings.Contains(issues[i].Body, marker) {
				return &issues[i], nil
			}
		}
		if len(issues) < 100 {
			break
		}
	}
	return nil, nil
}

// createOrFindIssue uses a marker in the issue body to recover from a response
// timeout. It never retries a failed read, and the caller stores uncertain
// create outcomes separately so an administrator must reconcile before retry.
func (c *githubIssueClient) createOrFindIssue(ctx context.Context, token string, action support.Action) (support.ExternalIssue, bool, bool, error) {
	marker := "<!-- boop-support-action:" + action.ID + " -->"
	existing, err := c.findIssue(ctx, token, action.Destination, marker)
	if err != nil {
		return support.ExternalIssue{}, false, false, err
	}
	if existing != nil {
		if !validGitHubIssueURL(action.Destination, existing.HTMLURL) {
			return support.ExternalIssue{}, false, false, errors.New("GitHub returned an issue URL outside the configured repository")
		}
		return support.ExternalIssue{ID: strconv.FormatInt(existing.ID, 10), URL: existing.HTMLURL}, true, false, nil
	}
	parts := strings.SplitN(action.Destination, "/", 2)
	endpoint := "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/issues"
	resp, err := c.request(ctx, token, http.MethodPost, endpoint, githubIssuePayload{Title: action.Title, Body: strings.TrimSpace(action.Body) + "\n\n" + marker})
	if err != nil {
		return support.ExternalIssue{}, false, true, errors.New("GitHub issue creation result is unknown; reconcile before retrying")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		uncertain := resp.StatusCode >= 500
		if uncertain {
			return support.ExternalIssue{}, false, true, fmt.Errorf("GitHub returned HTTP %d; issue creation result is unknown", resp.StatusCode)
		}
		return support.ExternalIssue{}, false, false, fmt.Errorf("GitHub rejected issue creation (HTTP %d)", resp.StatusCode)
	}
	var created githubIssue
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&created); err != nil {
		return support.ExternalIssue{}, false, true, errors.New("GitHub created the issue but its response could not be read; reconcile before retrying")
	}
	if created.ID == 0 || !validGitHubIssueURL(action.Destination, created.HTMLURL) {
		return support.ExternalIssue{}, false, true, errors.New("GitHub created an issue but returned an invalid result; reconcile before retrying")
	}
	return support.ExternalIssue{ID: strconv.FormatInt(created.ID, 10), URL: created.HTMLURL}, false, false, nil
}

func validGitHubIssueURL(repo, raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return strings.HasPrefix(parsed.Path, "/"+repo+"/issues/")
}
