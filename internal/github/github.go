// Package github retrieves pull request context from GitHub using read-only API calls.
package github

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	gh "github.com/google/go-github/v68/github"
	"golang.org/x/oauth2"
)

// CIStatus represents the aggregate state of a pull request's checks.
type CIStatus string

const (
	// CIPassing means all discovered checks passed.
	CIPassing CIStatus = "passing"
	// CIFailing means at least one discovered check failed.
	CIFailing CIStatus = "failing"
	// CIPending means at least one discovered check is pending.
	CIPending CIStatus = "pending"
	// CIUnknown means no status information is available.
	CIUnknown CIStatus = "unknown"
)

// PR contains the pull request fields used during triage.
type PR struct {
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	Author     string    `json:"author"`
	Labels     []string  `json:"labels"`
	Draft      bool      `json:"draft"`
	BaseBranch string    `json:"base_branch"`
	HeadSHA    string    `json:"head_sha"`
	URL        string    `json:"url"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListOptions controls pull request listing.
type ListOptions struct {
	Limit             int
	IncludeAllAuthors bool
}

// FileSummary describes one changed file and its line counts.
type FileSummary struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
}

// PRContext contains metadata and fetched change/check summaries for one PR.
type PRContext struct {
	PR             PR            `json:"pr"`
	ChangedFiles   []FileSummary `json:"changed_files"`
	Additions      int           `json:"additions"`
	Deletions      int           `json:"deletions"`
	CIStatus       CIStatus      `json:"ci_status"`
	DependencyName string        `json:"dependency_name"`
	FromVersion    string        `json:"from_version"`
	ToVersion      string        `json:"to_version"`
}

// Client provides read-only pull request operations.
type Client interface {
	ListOpenDependencyPRs(ctx context.Context, owner, repo string, opts ListOptions) ([]PR, error)
	FetchContext(ctx context.Context, owner, repo string, prNumber int) (PRContext, error)
}

// RateLimitError reports an exhausted GitHub API rate limit and its reset time.
type RateLimitError struct {
	ResetAt time.Time
}

// Error returns an actionable rate limit message.
func (e *RateLimitError) Error() string {
	if e.ResetAt.IsZero() {
		return "GitHub API rate limit exceeded; retry later"
	}
	return fmt.Sprintf("GitHub API rate limit exceeded; resets at %s", e.ResetAt.UTC().Format(time.RFC3339))
}

// GitHubClient is the google/go-github implementation of Client.
type GitHubClient struct{ client *gh.Client }

// NewClient creates an authenticated read-only GitHub API client.
func NewClient(ctx context.Context, token string) *GitHubClient {
	httpClient := http.DefaultClient
	if token != "" {
		httpClient = oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}))
	}
	return &GitHubClient{client: gh.NewClient(httpClient)}
}

// NewClientWithBaseURL creates a client configured for a GitHub-compatible API endpoint.
func NewClientWithBaseURL(ctx context.Context, token, baseURL string) (*GitHubClient, error) {
	client := NewClient(ctx, token)
	parsed, err := client.client.WithEnterpriseURLs(baseURL, baseURL)
	if err != nil {
		return nil, fmt.Errorf("configure GitHub API URL: %w", err)
	}
	client.client = parsed
	return client, nil
}

// NewClientFromEnvironment creates an authenticated client from GITHUB_TOKEN.
func NewClientFromEnvironment(ctx context.Context) (*GitHubClient, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN is required; create a classic PAT with public_repo (or repo) read scope")
	}
	return NewClient(ctx, token), nil
}

// ListOpenDependencyPRs lists open PRs, filtering to supported dependency bots by default.
func (c *GitHubClient) ListOpenDependencyPRs(ctx context.Context, owner, repo string, opts ListOptions) ([]PR, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 25
	}
	var result []PR
	page := 1
	for len(result) < limit {
		perPage := 100
		listOpts := &gh.PullRequestListOptions{State: "open", ListOptions: gh.ListOptions{Page: page, PerPage: perPage}}
		pulls, response, err := c.client.PullRequests.List(ctx, owner, repo, listOpts)
		if err != nil {
			return nil, c.wrapAPIError(err, response)
		}
		if len(pulls) == 0 {
			break
		}
		for _, pull := range pulls {
			if pull == nil {
				continue
			}
			pr := convertPR(pull)
			if !opts.IncludeAllAuthors && pr.Author != "dependabot[bot]" && pr.Author != "renovate[bot]" {
				continue
			}
			result = append(result, pr)
			if len(result) == limit {
				break
			}
		}
		if response == nil || response.NextPage == 0 || response.NextPage == page {
			break
		}
		page = response.NextPage
	}
	return result, nil
}

// FetchContext fetches PR metadata, changed files, and combined status/check-run state.
func (c *GitHubClient) FetchContext(ctx context.Context, owner, repo string, prNumber int) (PRContext, error) {
	pull, response, err := c.client.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		return PRContext{}, c.wrapAPIError(err, response)
	}
	pr := convertPR(pull)
	files := make([]FileSummary, 0)
	additions, deletions := 0, 0
	for page := 1; len(files) < 300; page++ {
		pageSize := min(100, 300-len(files))
		list, pageResponse, err := c.client.PullRequests.ListFiles(ctx, owner, repo, prNumber, &gh.ListOptions{Page: page, PerPage: pageSize})
		if err != nil {
			return PRContext{}, c.wrapAPIError(err, pageResponse)
		}
		for _, file := range list {
			if file == nil {
				continue
			}
			path := value(file.Filename)
			item := FileSummary{Path: path, Additions: value(file.Additions), Deletions: value(file.Deletions), Changes: value(file.Changes)}
			files = append(files, item)
			if !IsLockfile(path) {
				additions += item.Additions
				deletions += item.Deletions
			}
		}
		if len(list) == 0 || pageResponse == nil || pageResponse.NextPage == 0 {
			break
		}
	}
	ci, err := c.fetchCI(ctx, owner, repo, pr.HeadSHA)
	if err != nil {
		return PRContext{}, err
	}
	dep, from, to := ParseUpdate(pr.Title)
	return PRContext{PR: pr, ChangedFiles: files, Additions: additions, Deletions: deletions, CIStatus: ci, DependencyName: dep, FromVersion: from, ToVersion: to}, nil
}

func (c *GitHubClient) fetchCI(ctx context.Context, owner, repo, sha string) (CIStatus, error) {
	if sha == "" {
		return CIUnknown, nil
	}
	combined, response, statusErr := c.client.Repositories.GetCombinedStatus(ctx, owner, repo, sha, nil)
	if statusErr != nil && response != nil && response.StatusCode != http.StatusNotFound {
		return CIUnknown, c.wrapAPIError(statusErr, response)
	}
	checks, checkResponse, checksErr := c.client.Checks.ListCheckRunsForRef(ctx, owner, repo, sha, nil)
	if checksErr != nil && checkResponse != nil && checkResponse.StatusCode != http.StatusNotFound {
		return CIUnknown, c.wrapAPIError(checksErr, checkResponse)
	}
	seen, failed, pending := false, false, false
	if combined != nil {
		for _, status := range combined.Statuses {
			seen = true
			switch value(status.State) {
			case "failure", "error":
				failed = true
			case "pending":
				pending = true
			}
		}
	}
	if checks != nil {
		for _, check := range checks.CheckRuns {
			seen = true
			if value(check.Status) != "completed" {
				pending = true
				continue
			}
			switch value(check.Conclusion) {
			case "failure", "timed_out", "cancelled", "action_required", "startup_failure":
				failed = true
			case "success", "skipped", "neutral":
			default:
				pending = true
			}
		}
	}
	if failed {
		return CIFailing, nil
	}
	if pending {
		return CIPending, nil
	}
	if seen {
		return CIPassing, nil
	}
	return CIUnknown, nil
}

func (c *GitHubClient) wrapAPIError(err error, response *gh.Response) error {
	if response != nil && response.StatusCode == http.StatusForbidden && response.Header.Get("X-RateLimit-Remaining") == "0" {
		reset := time.Time{}
		if raw := response.Header.Get("X-RateLimit-Reset"); raw != "" {
			if seconds, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil {
				reset = time.Unix(seconds, 0)
			}
		}
		return &RateLimitError{ResetAt: reset}
	}
	return fmt.Errorf("GitHub API request failed: %w", err)
}

var updatePattern = regexp.MustCompile(`(?i)^\s*(?:bump\s+)?(.+?)\s+from\s+v?([0-9]+(?:\.[0-9]+){0,2}(?:[-+][0-9A-Za-z.-]+)?)\s+to\s+v?([0-9]+(?:\.[0-9]+){0,2}(?:[-+][0-9A-Za-z.-]+)?)`)

// ParseUpdate best-effort extracts a dependency and old/new versions from a PR title.
func ParseUpdate(title string) (dependency, from, to string) {
	match := updatePattern.FindStringSubmatch(title)
	if len(match) != 4 {
		return "", "", ""
	}
	return strings.TrimSpace(match[1]), match[2], match[3]
}

// IsLockfile reports whether path is a recognized dependency lockfile.
func IsLockfile(path string) bool {
	base := strings.ToLower(path[strings.LastIndex(path, "/")+1:])
	return base == "package-lock.json" || base == "yarn.lock" || base == "pnpm-lock.yaml" || base == "bun.lock" || base == "bun.lockb" || base == "cargo.lock" || base == "gemfile.lock" || base == "poetry.lock" || base == "pipfile.lock" || base == "composer.lock" || base == "go.sum" || base == "packages.lock.json" || strings.HasSuffix(base, ".lock")
}

func value[T any](pointer *T) T {
	if pointer == nil {
		var zero T
		return zero
	}
	return *pointer
}

func convertPR(pull *gh.PullRequest) PR {
	labels := make([]string, 0, len(pull.Labels))
	for _, label := range pull.Labels {
		if label != nil {
			labels = append(labels, value(label.Name))
		}
	}
	created := time.Time{}
	if pull.CreatedAt != nil {
		created = pull.CreatedAt.Time
	}
	author := ""
	if pull.User != nil {
		author = value(pull.User.Login)
	}
	base, head := "", ""
	if pull.Base != nil {
		base = value(pull.Base.Ref)
	}
	if pull.Head != nil {
		head = value(pull.Head.SHA)
	}
	return PR{Number: value(pull.Number), Title: value(pull.Title), Body: value(pull.Body), Author: author, Labels: labels, Draft: value(pull.Draft), BaseBranch: base, HeadSHA: head, URL: value(pull.HTMLURL), CreatedAt: created}
}
