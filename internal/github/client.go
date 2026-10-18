package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNotModified is returned when GitHub answers 304 because the cached ETag
// still matches. It is not a failure: the caller should keep the data it has.
var ErrNotModified = errors.New("github: not modified")

// ErrStatsComputing is returned when /stats/contributors answers 202, meaning
// GitHub is still building the statistics cache. Callers must retry rather than
// treat the empty body as "no contributors" -- doing so would render an empty
// leaderboard that looks identical to a real one.
var ErrStatsComputing = errors.New("github: statistics still computing, retry")

const (
	defaultBaseURL = "https://api.github.com"
	apiVersion     = "2022-11-28"
	userAgent      = "hushield-website"
)

// Client is a read-only GitHub API client. It is safe for concurrent use.
//
// ETags are remembered per endpoint and replayed as If-None-Match, so an
// unchanged resource costs nothing against the rate limit. That is what keeps
// the polling budget (~40 req/hr) far below the authenticated ceiling of 5,000.
type Client struct {
	baseURL string
	token   string
	owner   string
	repo    string
	http    *http.Client

	mu        sync.Mutex
	etags     map[string]string
	rateLimit RateLimit
}

// RateLimit is the most recently observed GitHub API budget.
type RateLimit struct {
	Remaining int
	Reset     time.Time
}

// RateLimit returns the budget observed on the latest GitHub response.
func (c *Client) RateLimit() RateLimit { c.mu.Lock(); defer c.mu.Unlock(); return c.rateLimit }

// Option customizes a Client.
type Option func(*Client)

// WithBaseURL points the client at an alternate API host. Used by tests to
// target an httptest server.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// WithHTTPClient supplies a custom http.Client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// New returns a Client for owner/repo. An empty token still works but is
// limited to 60 requests per hour, which is not enough for the poll schedule.
func New(owner, repo, token string, opts ...Option) *Client {
	c := &Client{
		baseURL: defaultBaseURL,
		token:   token,
		owner:   owner,
		repo:    repo,
		http:    &http.Client{Timeout: 20 * time.Second},
		etags:   make(map[string]string),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// get performs a single authenticated GET and decodes the body into out.
//
// A 304 yields ErrNotModified and leaves out untouched. A 202 from a statistics
// endpoint yields ErrStatsComputing. Any other non-2xx is an error carrying the
// status so the caller can log something actionable.
func (c *Client) get(ctx context.Context, path string, out any) error {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("github: build request for %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	c.mu.Lock()
	prev := c.etags[path]
	c.mu.Unlock()
	if prev != "" {
		req.Header.Set("If-None-Match", prev)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("github: GET %s: %w", path, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	// GitHub sends these headers on successful, conditional, and error responses.
	// Record them before classifying the response so operators can see depletion
	// before the API starts refusing requests.
	c.mu.Lock()
	if remaining, err := strconv.Atoi(resp.Header.Get("X-RateLimit-Remaining")); err == nil {
		c.rateLimit.Remaining = remaining
	}
	if reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		c.rateLimit.Reset = time.Unix(reset, 0)
	}
	c.mu.Unlock()

	switch {
	case resp.StatusCode == http.StatusNotModified:
		return ErrNotModified

	// 202 means GitHub accepted the request but the stats cache is not built
	// yet. The body is empty; decoding it would silently produce zero results.
	case resp.StatusCode == http.StatusAccepted:
		return ErrStatsComputing

	case resp.StatusCode < 200 || resp.StatusCode > 299:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("github: GET %s: unexpected status %d: %s",
			path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("github: decode %s: %w", path, err)
	}

	if tag := resp.Header.Get("ETag"); tag != "" {
		c.mu.Lock()
		c.etags[path] = tag
		c.mu.Unlock()
	}
	return nil
}

func (c *Client) repoPath(suffix string) string {
	return fmt.Sprintf("/repos/%s/%s%s", c.owner, c.repo, suffix)
}

// Repo fetches the repository summary.
func (c *Client) Repo(ctx context.Context) (Repo, error) {
	var r Repo
	err := c.get(ctx, c.repoPath(""), &r)
	return r, err
}

// Releases fetches published releases, newest first, excluding drafts.
func (c *Client) Releases(ctx context.Context) ([]Release, error) {
	var all []Release
	if err := c.get(ctx, c.repoPath("/releases?per_page=50"), &all); err != nil {
		return nil, err
	}
	out := make([]Release, 0, len(all))
	for _, r := range all {
		if !r.Draft {
			out = append(out, r)
		}
	}
	return out, nil
}

// Issues fetches open issues with pull requests filtered out. GitHub returns
// PRs from this endpoint as well, and counting them as issues would overstate
// the open-issue count on every page that shows it.
func (c *Client) Issues(ctx context.Context) ([]Issue, error) {
	var all []Issue
	if err := c.get(ctx, c.repoPath("/issues?state=open&per_page=100&sort=created&direction=desc"), &all); err != nil {
		return nil, err
	}
	out := make([]Issue, 0, len(all))
	for _, i := range all {
		if !i.IsPullRequest() {
			out = append(out, i)
		}
	}
	return out, nil
}

// PullRequests fetches recent PRs in any state, so both the open queue and
// recently-merged work are available.
func (c *Client) PullRequests(ctx context.Context) ([]PullRequest, error) {
	var prs []PullRequest
	err := c.get(ctx, c.repoPath("/pulls?state=all&per_page=100&sort=updated&direction=desc"), &prs)
	return prs, err
}

// Contributors fetches the contributor list ordered by commit count.
func (c *Client) Contributors(ctx context.Context) ([]Contributor, error) {
	var cs []Contributor
	err := c.get(ctx, c.repoPath("/contributors?per_page=100"), &cs)
	return cs, err
}

// ContributorStats fetches per-contributor weekly additions and deletions.
//
// This endpoint is computed asynchronously by GitHub and answers 202 while the
// cache builds, which surfaces as ErrStatsComputing.
func (c *Client) ContributorStats(ctx context.Context) ([]ContributorStats, error) {
	var s []ContributorStats
	err := c.get(ctx, c.repoPath("/stats/contributors"), &s)
	return s, err
}
