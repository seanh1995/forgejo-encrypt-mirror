// Package github implements a minimal REST API client for GitHub, used to
// ensure an encrypted mirror's destination repository exists before the
// encrypted git history is pushed to it.
package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	gitengine "github.com/seanh1995/forgejo-encrypt-mirror/internal/git"
)

// DefaultBaseURL is the GitHub REST API endpoint used when Client.BaseURL
// is empty. It can be overridden (e.g. in Client construction) to point at
// a GitHub Enterprise Server instance's API root.
const DefaultBaseURL = "https://api.github.com"

// Client is a small REST client for the subset of the GitHub API needed to
// create and inspect repositories.
type Client struct {
	// BaseURL is the root of the GitHub REST API, e.g.
	// "https://api.github.com". Defaults to DefaultBaseURL if empty.
	BaseURL string
	// Token is a GitHub personal access token (classic or fine-grained)
	// sent as a bearer token on every request.
	Token string

	HTTPClient *http.Client
}

// NewClient creates a Client authenticating with token against the public
// GitHub API.
func NewClient(token string) *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		Token:      token,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// apiError is returned when the GitHub API responds with a non-2xx status.
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("github: unexpected status %d: %s", e.Status, e.Body)
}

// do sends an authenticated request with an optional JSON body and decodes
// a JSON response into out (if non-nil). It returns the HTTP status code.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) (int, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("github: encode request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, reqBody)
	if err != nil {
		return 0, fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("github: request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, fmt.Errorf("github: read response: %w", err)
	}

	if resp.StatusCode >= 300 {
		return resp.StatusCode, &apiError{Status: resp.StatusCode, Body: string(data)}
	}

	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("github: decode response: %w", err)
		}
	}

	return resp.StatusCode, nil
}

// RepositoryExists reports whether owner/repo already exists on GitHub.
func (c *Client) RepositoryExists(ctx context.Context, owner, repo string) (bool, error) {
	if err := gitengine.ValidateName("owner", owner); err != nil {
		return false, err
	}
	if err := gitengine.ValidateName("repo", repo); err != nil {
		return false, err
	}

	status, err := c.do(ctx, http.MethodGet, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo), nil, nil)
	if err != nil {
		var apiErr *apiError
		if isAPIError(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return false, nil
		}
		return false, fmt.Errorf("github: check repository %s/%s: %w", owner, repo, err)
	}
	return status == http.StatusOK, nil
}

// account describes the minimal fields needed from GET /users/{owner} to
// tell whether owner is an organization or a regular user account, since
// GitHub uses different endpoints to create repositories under each.
type account struct {
	Type string `json:"type"`
}

// createRepoRequest is the JSON body sent to GitHub's repository creation
// endpoints.
type createRepoRequest struct {
	Name    string `json:"name"`
	Private bool   `json:"private"`
}

// CreateRepository creates a new GitHub repository named repo under owner.
// owner may be the authenticated user's own account or an organization
// the token has repo-creation access to.
func (c *Client) CreateRepository(ctx context.Context, owner, repo string, private bool) error {
	if err := gitengine.ValidateName("owner", owner); err != nil {
		return err
	}
	if err := gitengine.ValidateName("repo", repo); err != nil {
		return err
	}

	var acc account
	if _, err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(owner), nil, &acc); err != nil {
		return fmt.Errorf("github: look up owner %s: %w", owner, err)
	}

	reqBody := createRepoRequest{Name: repo, Private: private}

	path := "/user/repos"
	if acc.Type == "Organization" {
		path = "/orgs/" + url.PathEscape(owner) + "/repos"
	}

	if _, err := c.do(ctx, http.MethodPost, path, reqBody, nil); err != nil {
		return fmt.Errorf("github: create repository %s/%s: %w", owner, repo, err)
	}
	return nil
}

// EnsureRepository makes sure owner/repo exists on GitHub, creating it
// (with the given visibility) if it does not.
func (c *Client) EnsureRepository(ctx context.Context, owner, repo string, private bool) error {
	exists, err := c.RepositoryExists(ctx, owner, repo)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return c.CreateRepository(ctx, owner, repo, private)
}

// RepoURL returns the HTTPS clone URL for owner/repo on GitHub.
func RepoURL(owner, repo string) string {
	return "https://github.com/" + owner + "/" + repo + ".git"
}

// BasicAuthHeader builds an HTTP Basic Authorization header value for
// authenticating git operations against GitHub with a personal access
// token, using the "x-access-token" placeholder username GitHub accepts
// in place of an actual username. This is the same scheme GitHub's own
// tooling uses for token-based git HTTPS auth.
func BasicAuthHeader(token string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
}

// isAPIError reports whether err is (or wraps) an *apiError, writing it
// into target on success.
func isAPIError(err error, target **apiError) bool {
	ae, ok := err.(*apiError)
	if !ok {
		return false
	}
	*target = ae
	return true
}
