// Package github provides a minimal, typed, read-only client for the subset of
// the GitHub REST API this site displays. GitHub's JSON shapes are decoded here
// and nowhere else, so an upstream field change breaks a test in this package
// rather than leaking into templates.
package github

import "time"

// Repo is the repository summary shown in the live stats strip.
type Repo struct {
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Stars       int    `json:"stargazers_count"`
	Forks       int    `json:"forks_count"`
	OpenIssues  int    `json:"open_issues_count"`
	Watchers    int    `json:"subscribers_count"`
}

// Release is one published GitHub release, rendered on /changelog.
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

// User is the minimal actor representation used across issues, PRs, and the
// contributor list.
type User struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
}

// Label is an issue label. Only the name and color are used.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Issue is an open issue. GitHub returns pull requests from the issues
// endpoint too; PullRequest is non-nil for those and they are filtered out.
type Issue struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	HTMLURL     string    `json:"html_url"`
	State       string    `json:"state"`
	Labels      []Label   `json:"labels"`
	User        User      `json:"user"`
	Comments    int       `json:"comments"`
	CreatedAt   time.Time `json:"created_at"`
	Assignee    *User     `json:"assignee"`
	PullRequest *struct{} `json:"pull_request"`
}

// HasLabel reports whether the issue carries the named label.
func (i Issue) HasLabel(name string) bool {
	for _, l := range i.Labels {
		if l.Name == name {
			return true
		}
	}
	return false
}

// IsPullRequest reports whether this "issue" is really a pull request. The
// issues endpoint returns both, and conflating them inflates the issue count.
func (i Issue) IsPullRequest() bool { return i.PullRequest != nil }

// PullRequest is an open or recently-merged PR. MergedAt is nil unless merged,
// which is how the leaderboard distinguishes a merged PR from a closed one.
type PullRequest struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	HTMLURL   string     `json:"html_url"`
	State     string     `json:"state"`
	User      User       `json:"user"`
	CreatedAt time.Time  `json:"created_at"`
	MergedAt  *time.Time `json:"merged_at"`
}

// IsMerged reports whether the PR was actually merged, as opposed to closed
// without merging. Ranking on "closed" would credit rejected work.
func (p PullRequest) IsMerged() bool { return p.MergedAt != nil }

// Contributor is one entry from the contributors endpoint.
type Contributor struct {
	User
	Contributions int `json:"contributions"`
}

// ContributorStats is one entry from /stats/contributors, which carries the
// per-week additions and deletions used for the lines-changed stat.
type ContributorStats struct {
	Author User `json:"author"`
	Total  int  `json:"total"`
	Weeks  []struct {
		Week      int64 `json:"w"`
		Additions int   `json:"a"`
		Deletions int   `json:"d"`
		Commits   int   `json:"c"`
	} `json:"weeks"`
}

// Lines sums additions and deletions across all weeks.
func (c ContributorStats) Lines() (added, deleted int) {
	for _, w := range c.Weeks {
		added += w.Additions
		deleted += w.Deletions
	}
	return added, deleted
}

// FirstCommit returns the week of this author's first recorded commit, or the
// zero time when they have no commits. Used for the "first-time contributors"
// section.
func (c ContributorStats) FirstCommit() time.Time {
	for _, w := range c.Weeks {
		if w.Commits > 0 {
			return time.Unix(w.Week, 0).UTC()
		}
	}
	return time.Time{}
}
