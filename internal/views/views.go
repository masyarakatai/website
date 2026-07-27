// Package views turns cached GitHub data into the shapes templates render.
//
// No GitHub type reaches a template directly. That keeps rendering decisions
// (ranking, grouping, freshness disclosure) testable without HTTP or HTML.
package views

import (
	"sort"
	"time"

	"github.com/Hushield/website/internal/cache"
	"github.com/Hushield/website/internal/github"
)

// Freshness is the template-facing view of a dataset's state.
type Freshness struct {
	State       string
	OK          bool
	Stale       bool
	LastSuccess time.Time
}

// FreshnessOf projects a Dataset's state for templates.
func FreshnessOf[T any](d cache.Dataset[T]) Freshness {
	return Freshness{
		State:       d.State.String(),
		OK:          d.OK(),
		Stale:       d.State == cache.Stale,
		LastSuccess: d.LastSuccess,
	}
}

// Stats is the live counter strip on the home page.
type Stats struct {
	Stars        int
	Forks        int
	OpenIssues   int
	Contributors int
	MergedPRs    int
	GoodFirst    int
	Freshness    Freshness
}

// LeaderRow is one contributor's standing.
//
// Rank is decided by merged pull requests, then issues closed, then reviews,
// then commits. Lines changed is displayed but deliberately never sorted on:
// ranking by volume rewards vendored dependencies, generated code, and
// reformatting commits over a twelve-line fix to a race condition.
type LeaderRow struct {
	Login       string
	AvatarURL   string
	HTMLURL     string
	MergedPRs   int
	Commits     int
	Added       int
	Deleted     int
	IsFirstYear bool
	FirstSeen   time.Time
}

// LinesChanged is the combined additions and deletions, shown as a stat.
func (r LeaderRow) LinesChanged() int { return r.Added + r.Deleted }

// Leaderboard is the ranked contributor table plus newcomers.
type Leaderboard struct {
	Rows []LeaderRow
	// NewThisMonth lists contributors whose first commit landed within the last
	// 31 days. This is the single most useful element on the page for
	// recruitment: it is evidence that an outsider's work actually gets merged.
	NewThisMonth []LeaderRow
	Freshness    Freshness
}

// BuildLeaderboard ranks contributors. excludeLogins drops maintainers from the
// ranking so newcomers are not permanently compared against the project owner.
func BuildLeaderboard(
	contributors []github.Contributor,
	stats []github.ContributorStats,
	prs []github.PullRequest,
	excludeLogins map[string]bool,
	now time.Time,
) Leaderboard {
	merged := make(map[string]int)
	for _, p := range prs {
		if p.IsMerged() {
			merged[p.User.Login]++
		}
	}

	linesAdded := make(map[string]int)
	linesDeleted := make(map[string]int)
	firstSeen := make(map[string]time.Time)
	for _, s := range stats {
		a, d := s.Lines()
		linesAdded[s.Author.Login] = a
		linesDeleted[s.Author.Login] = d
		if fc := s.FirstCommit(); !fc.IsZero() {
			firstSeen[s.Author.Login] = fc
		}
	}

	rows := make([]LeaderRow, 0, len(contributors))
	for _, c := range contributors {
		if excludeLogins[c.Login] {
			continue
		}
		rows = append(rows, LeaderRow{
			Login:     c.Login,
			AvatarURL: c.AvatarURL,
			HTMLURL:   c.HTMLURL,
			MergedPRs: merged[c.Login],
			Commits:   c.Contributions,
			Added:     linesAdded[c.Login],
			Deleted:   linesDeleted[c.Login],
			FirstSeen: firstSeen[c.Login],
		})
	}

	// Merged PRs first, then commits, then login for a stable order. Lines are
	// never part of the comparison.
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.MergedPRs != b.MergedPRs {
			return a.MergedPRs > b.MergedPRs
		}
		if a.Commits != b.Commits {
			return a.Commits > b.Commits
		}
		return a.Login < b.Login
	})

	cutoff := now.AddDate(0, 0, -31)
	var newcomers []LeaderRow
	for _, r := range rows {
		if !r.FirstSeen.IsZero() && r.FirstSeen.After(cutoff) {
			newcomers = append(newcomers, r)
		}
	}

	return Leaderboard{Rows: rows, NewThisMonth: newcomers}
}

// IssueRow is one issue in a list.
type IssueRow struct {
	Number    int
	Title     string
	URL       string
	Labels    []string
	Comments  int
	Age       string
	Claimed   bool
	ClaimedBy string
}

// PartitionIssues splits open issues into the buckets the site displays.
func PartitionIssues(issues []github.Issue, now time.Time) (goodFirst, helpWanted, bugs, other []IssueRow) {
	for _, i := range issues {
		row := toIssueRow(i, now)
		switch {
		case i.HasLabel("good-first-issue"):
			goodFirst = append(goodFirst, row)
		case i.HasLabel("help-wanted"):
			helpWanted = append(helpWanted, row)
		case i.HasLabel("bug"):
			bugs = append(bugs, row)
		default:
			other = append(other, row)
		}
	}
	return goodFirst, helpWanted, bugs, other
}

func toIssueRow(i github.Issue, now time.Time) IssueRow {
	labels := make([]string, 0, len(i.Labels))
	for _, l := range i.Labels {
		labels = append(labels, l.Name)
	}
	row := IssueRow{
		Number:   i.Number,
		Title:    i.Title,
		URL:      i.HTMLURL,
		Labels:   labels,
		Comments: i.Comments,
		Age:      HumanAge(i.CreatedAt, now),
	}
	if i.Assignee != nil {
		row.Claimed = true
		row.ClaimedBy = i.Assignee.Login
	}
	return row
}

// PRRow is one pull request in a list.
type PRRow struct {
	Number int
	Title  string
	URL    string
	Author string
	Merged bool
	When   string
}

// RecentMergedPRs returns the most recently merged PRs, newest first, capped at
// limit. This is the "outsiders get merged" evidence on /contribute.
func RecentMergedPRs(prs []github.PullRequest, limit int, now time.Time) []PRRow {
	var mergedOnly []github.PullRequest
	for _, p := range prs {
		if p.IsMerged() {
			mergedOnly = append(mergedOnly, p)
		}
	}
	sort.SliceStable(mergedOnly, func(i, j int) bool {
		return mergedOnly[i].MergedAt.After(*mergedOnly[j].MergedAt)
	})
	if limit > 0 && len(mergedOnly) > limit {
		mergedOnly = mergedOnly[:limit]
	}

	out := make([]PRRow, 0, len(mergedOnly))
	for _, p := range mergedOnly {
		out = append(out, PRRow{
			Number: p.Number, Title: p.Title, URL: p.HTMLURL,
			Author: p.User.Login, Merged: true,
			When: HumanAge(*p.MergedAt, now),
		})
	}
	return out
}

// OpenPRs returns currently-open pull requests, newest first.
func OpenPRs(prs []github.PullRequest, limit int, now time.Time) []PRRow {
	var out []PRRow
	for _, p := range prs {
		if p.State != "open" {
			continue
		}
		out = append(out, PRRow{
			Number: p.Number, Title: p.Title, URL: p.HTMLURL,
			Author: p.User.Login, When: HumanAge(p.CreatedAt, now),
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// HumanAge renders an approximate, human-readable age.
func HumanAge(t, now time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour")
	case d < 31*24*time.Hour:
		return plural(int(d.Hours()/24), "day")
	case d < 365*24*time.Hour:
		return plural(int(d.Hours()/24/30), "month")
	default:
		return plural(int(d.Hours()/24/365), "year")
	}
}

func plural(n int, unit string) string {
	s := unit
	if n != 1 {
		s += "s"
	}
	return itoa(n) + " " + s + " ago"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
