package views

import (
	"testing"
	"time"

	"github.com/Hushield/website/internal/github"
)

func ts(t time.Time) *time.Time { return &t }

var now = time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)

// The defining property of the leaderboard: merged PRs decide the order, and a
// huge line count never overtakes them. If this test ever fails because someone
// "improved" the sort, the page has started rewarding volume over value.
func TestBuildLeaderboard_RanksByMergedPRsNotLines(t *testing.T) {
	contributors := []github.Contributor{
		{User: github.User{Login: "bulk"}, Contributions: 200},
		{User: github.User{Login: "surgical"}, Contributions: 4},
	}
	// "bulk" changed 500k lines with no merged PRs (think a vendored dependency
	// or a reformat). "surgical" merged three PRs touching 30 lines.
	stats := []github.ContributorStats{
		{Author: github.User{Login: "bulk"}, Weeks: []struct {
			Week      int64 `json:"w"`
			Additions int   `json:"a"`
			Deletions int   `json:"d"`
			Commits   int   `json:"c"`
		}{{Week: now.AddDate(-1, 0, 0).Unix(), Additions: 400000, Deletions: 100000, Commits: 200}}},
		{Author: github.User{Login: "surgical"}, Weeks: []struct {
			Week      int64 `json:"w"`
			Additions int   `json:"a"`
			Deletions int   `json:"d"`
			Commits   int   `json:"c"`
		}{{Week: now.AddDate(-1, 0, 0).Unix(), Additions: 20, Deletions: 10, Commits: 4}}},
	}
	prs := []github.PullRequest{
		{User: github.User{Login: "surgical"}, MergedAt: ts(now.Add(-24 * time.Hour))},
		{User: github.User{Login: "surgical"}, MergedAt: ts(now.Add(-48 * time.Hour))},
		{User: github.User{Login: "surgical"}, MergedAt: ts(now.Add(-72 * time.Hour))},
	}

	board := BuildLeaderboard(contributors, stats, prs, nil, now)

	if len(board.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(board.Rows))
	}
	if board.Rows[0].Login != "surgical" {
		t.Errorf("first place = %q, want surgical -- merged PRs must outrank line count", board.Rows[0].Login)
	}
	if board.Rows[0].MergedPRs != 3 {
		t.Errorf("surgical MergedPRs = %d, want 3", board.Rows[0].MergedPRs)
	}
	// Lines are still reported, just not ranked on.
	if board.Rows[1].LinesChanged() != 500000 {
		t.Errorf("bulk LinesChanged = %d, want 500000 shown as a stat", board.Rows[1].LinesChanged())
	}
}

// Closed-but-unmerged PRs must not count. Crediting them would reward rejected
// work and let anyone inflate their rank by opening and closing PRs.
func TestBuildLeaderboard_IgnoresUnmergedPRs(t *testing.T) {
	contributors := []github.Contributor{{User: github.User{Login: "a"}, Contributions: 1}}
	prs := []github.PullRequest{
		{User: github.User{Login: "a"}, State: "closed", MergedAt: nil},
		{User: github.User{Login: "a"}, State: "open", MergedAt: nil},
		{User: github.User{Login: "a"}, State: "closed", MergedAt: ts(now)},
	}

	board := BuildLeaderboard(contributors, nil, prs, nil, now)
	if got := board.Rows[0].MergedPRs; got != 1 {
		t.Errorf("MergedPRs = %d, want 1 (only the genuinely merged PR)", got)
	}
}

func TestBuildLeaderboard_ExcludesMaintainers(t *testing.T) {
	contributors := []github.Contributor{
		{User: github.User{Login: "jbrahy"}, Contributions: 500},
		{User: github.User{Login: "newperson"}, Contributions: 2},
	}
	board := BuildLeaderboard(contributors, nil, nil, map[string]bool{"jbrahy": true}, now)

	if len(board.Rows) != 1 {
		t.Fatalf("rows = %d, want 1 after exclusion", len(board.Rows))
	}
	if board.Rows[0].Login != "newperson" {
		t.Errorf("row = %q, want newperson", board.Rows[0].Login)
	}
}

func TestBuildLeaderboard_NewThisMonth(t *testing.T) {
	week := func(unix int64, commits int) []struct {
		Week      int64 `json:"w"`
		Additions int   `json:"a"`
		Deletions int   `json:"d"`
		Commits   int   `json:"c"`
	} {
		return []struct {
			Week      int64 `json:"w"`
			Additions int   `json:"a"`
			Deletions int   `json:"d"`
			Commits   int   `json:"c"`
		}{{Week: unix, Commits: commits}}
	}

	contributors := []github.Contributor{
		{User: github.User{Login: "veteran"}, Contributions: 90},
		{User: github.User{Login: "rookie"}, Contributions: 1},
	}
	stats := []github.ContributorStats{
		{Author: github.User{Login: "veteran"}, Weeks: week(now.AddDate(-2, 0, 0).Unix(), 90)},
		{Author: github.User{Login: "rookie"}, Weeks: week(now.AddDate(0, 0, -5).Unix(), 1)},
	}

	board := BuildLeaderboard(contributors, stats, nil, nil, now)

	if len(board.NewThisMonth) != 1 {
		t.Fatalf("NewThisMonth = %d, want 1", len(board.NewThisMonth))
	}
	if board.NewThisMonth[0].Login != "rookie" {
		t.Errorf("newcomer = %q, want rookie", board.NewThisMonth[0].Login)
	}
}

func TestPartitionIssues(t *testing.T) {
	issues := []github.Issue{
		{Number: 1, Labels: []github.Label{{Name: "good-first-issue"}}},
		{Number: 2, Labels: []github.Label{{Name: "help-wanted"}}},
		{Number: 3, Labels: []github.Label{{Name: "bug"}}},
		{Number: 4, Labels: []github.Label{{Name: "enhancement"}}},
		// Carries both; good-first-issue wins so it surfaces where newcomers look.
		{Number: 5, Labels: []github.Label{{Name: "help-wanted"}, {Name: "good-first-issue"}}},
	}
	gf, hw, bugs, other := PartitionIssues(issues, now)

	if len(gf) != 2 {
		t.Errorf("goodFirst = %d, want 2", len(gf))
	}
	if len(hw) != 1 {
		t.Errorf("helpWanted = %d, want 1", len(hw))
	}
	if len(bugs) != 1 {
		t.Errorf("bugs = %d, want 1", len(bugs))
	}
	if len(other) != 1 {
		t.Errorf("other = %d, want 1", len(other))
	}
}

func TestPartitionIssues_ClaimedByAssignee(t *testing.T) {
	issues := []github.Issue{{
		Number:   7,
		Labels:   []github.Label{{Name: "good-first-issue"}},
		Assignee: &github.User{Login: "someone"},
	}}
	gf, _, _, _ := PartitionIssues(issues, now)

	if !gf[0].Claimed || gf[0].ClaimedBy != "someone" {
		t.Errorf("Claimed=%v by %q, want true by someone", gf[0].Claimed, gf[0].ClaimedBy)
	}
}

func TestRecentMergedPRs_NewestFirstAndCapped(t *testing.T) {
	prs := []github.PullRequest{
		{Number: 1, MergedAt: ts(now.Add(-72 * time.Hour))},
		{Number: 2, MergedAt: nil, State: "open"},
		{Number: 3, MergedAt: ts(now.Add(-1 * time.Hour))},
		{Number: 4, MergedAt: ts(now.Add(-24 * time.Hour))},
	}
	got := RecentMergedPRs(prs, 2, now)

	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (capped)", len(got))
	}
	if got[0].Number != 3 || got[1].Number != 4 {
		t.Errorf("order = %d,%d, want 3,4 (newest merged first)", got[0].Number, got[1].Number)
	}
}

func TestOpenPRs_ExcludesMerged(t *testing.T) {
	prs := []github.PullRequest{
		{Number: 1, State: "open"},
		{Number: 2, State: "closed", MergedAt: ts(now)},
	}
	got := OpenPRs(prs, 0, now)
	if len(got) != 1 || got[0].Number != 1 {
		t.Errorf("got %+v, want only the open PR #1", got)
	}
}

func TestHumanAge(t *testing.T) {
	for _, tc := range []struct {
		name string
		when time.Time
		want string
	}{
		{"zero", time.Time{}, "unknown"},
		{"seconds", now.Add(-30 * time.Second), "just now"},
		{"one minute", now.Add(-1 * time.Minute), "1 minute ago"},
		{"minutes", now.Add(-5 * time.Minute), "5 minutes ago"},
		{"one hour", now.Add(-1 * time.Hour), "1 hour ago"},
		{"days", now.Add(-72 * time.Hour), "3 days ago"},
		{"months", now.AddDate(0, -3, 0), "3 months ago"},
		{"years", now.AddDate(-2, 0, 0), "2 years ago"},
	} {
		if got := HumanAge(tc.when, now); got != tc.want {
			t.Errorf("%s: HumanAge = %q, want %q", tc.name, got, tc.want)
		}
	}
}
