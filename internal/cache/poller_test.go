package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hushield/website/internal/github"
)

// fakeSource is a programmable Source. Each field decides what that endpoint
// returns, so a test can make exactly one dataset fail.
type fakeSource struct {
	repo     github.Repo
	repoErr  error
	releases []github.Release
	relErr   error
	issues   []github.Issue
	issueErr error
	prs      []github.PullRequest
	prErr    error
	contribs []github.Contributor
	conErr   error
	stats    []github.ContributorStats
	statsErr error
}

func (f *fakeSource) Repo(context.Context) (github.Repo, error) { return f.repo, f.repoErr }
func (f *fakeSource) Releases(context.Context) ([]github.Release, error) {
	return f.releases, f.relErr
}
func (f *fakeSource) Issues(context.Context) ([]github.Issue, error) { return f.issues, f.issueErr }
func (f *fakeSource) PullRequests(context.Context) ([]github.PullRequest, error) {
	return f.prs, f.prErr
}
func (f *fakeSource) Contributors(context.Context) ([]github.Contributor, error) {
	return f.contribs, f.conErr
}
func (f *fakeSource) ContributorStats(context.Context) ([]github.ContributorStats, error) {
	return f.stats, f.statsErr
}

func TestPoller_RefreshAllPublishesEverything(t *testing.T) {
	src := &fakeSource{
		repo:     github.Repo{FullName: "Hushield/hushield", Stars: 12},
		releases: []github.Release{{TagName: "v0.1.0"}},
		issues:   []github.Issue{{Number: 1, Title: "a"}},
		prs:      []github.PullRequest{{Number: 2}},
		contribs: []github.Contributor{{User: github.User{Login: "jbrahy"}, Contributions: 42}},
		stats:    []github.ContributorStats{{Author: github.User{Login: "jbrahy"}}},
	}
	snaps := NewSnapshots()
	NewPoller(src, snaps, Intervals{}, nil).RefreshAll(context.Background())

	if got := snaps.Repo.Load(); got.State != Fresh || got.Value.Stars != 12 {
		t.Errorf("repo = %+v, want Fresh with 12 stars", got)
	}
	if got := snaps.Releases.Load(); got.State != Fresh || len(got.Value) != 1 {
		t.Errorf("releases = %+v, want Fresh with 1 entry", got)
	}
	if got := snaps.Issues.Load(); got.State != Fresh || len(got.Value) != 1 {
		t.Errorf("issues = %+v, want Fresh with 1 entry", got)
	}
	if got := snaps.Contributors.Load(); got.State != Fresh || len(got.Value) != 1 {
		t.Errorf("contributors = %+v, want Fresh with 1 entry", got)
	}
}

// One failing dataset must not affect the others. Without this, a single flaky
// endpoint takes down every page.
func TestPoller_OneFailureIsIsolated(t *testing.T) {
	src := &fakeSource{
		repo:     github.Repo{Stars: 3},
		issues:   []github.Issue{{Number: 1}},
		statsErr: errors.New("github: 500"),
	}
	snaps := NewSnapshots()
	NewPoller(src, snaps, Intervals{}, nil).RefreshAll(context.Background())

	if got := snaps.Repo.Load(); got.State != Fresh {
		t.Errorf("repo state = %v, want Fresh despite the stats failure", got.State)
	}
	if got := snaps.Stats.Load(); got.State != Unavailable {
		t.Errorf("stats state = %v, want Unavailable (failed with no prior success)", got.State)
	}
}

// ErrStatsComputing means GitHub is still building the statistics cache and the
// body is empty. Publishing that empty body would render an empty leaderboard
// indistinguishable from a real one, so the poller must leave the snapshot
// alone and retry later.
func TestPoller_StatsComputingDoesNotPublishEmpty(t *testing.T) {
	snaps := NewSnapshots()
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	snaps.Stats.Publish([]github.ContributorStats{
		{Author: github.User{Login: "jbrahy"}},
	}, now)

	src := &fakeSource{statsErr: github.ErrStatsComputing}
	NewPoller(src, snaps, Intervals{}, nil).RefreshAll(context.Background())

	got := snaps.Stats.Load()
	if got.State != Fresh {
		t.Errorf("state = %v, want Fresh -- a 202 is retryable, not a failure", got.State)
	}
	if len(got.Value) != 1 {
		t.Fatalf("Value length = %d, want the previous 1 entry retained, not overwritten with empty", len(got.Value))
	}
}

// A 202 with no prior data must stay Unavailable rather than becoming an empty
// Fresh result.
func TestPoller_StatsComputingFromColdStaysUnavailable(t *testing.T) {
	snaps := NewSnapshots()
	src := &fakeSource{statsErr: github.ErrStatsComputing}
	NewPoller(src, snaps, Intervals{}, nil).RefreshAll(context.Background())

	if got := snaps.Stats.Load(); got.State != Unavailable {
		t.Errorf("state = %v, want Unavailable", got.State)
	}
}

// A 304 must keep data Fresh, not demote it to Stale. Most polls return 304, so
// getting this wrong marks the site permanently stale.
func TestPoller_NotModifiedKeepsFresh(t *testing.T) {
	snaps := NewSnapshots()
	t0 := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	snaps.Repo.Publish(github.Repo{Stars: 7}, t0)

	src := &fakeSource{repoErr: github.ErrNotModified}
	NewPoller(src, snaps, Intervals{}, nil).RefreshAll(context.Background())

	got := snaps.Repo.Load()
	if got.State != Fresh {
		t.Errorf("state = %v, want Fresh after 304", got.State)
	}
	if got.Value.Stars != 7 {
		t.Errorf("Stars = %d, want the cached 7 preserved", got.Value.Stars)
	}
}

// A cancelled context is shutdown, not a data problem; snapshots must survive
// untouched so a restart does not blank the site mid-drain.
func TestPoller_CancelledContextLeavesSnapshotIntact(t *testing.T) {
	snaps := NewSnapshots()
	t0 := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	snaps.Repo.Publish(github.Repo{Stars: 5}, t0)

	src := &fakeSource{repoErr: context.Canceled}
	NewPoller(src, snaps, Intervals{}, nil).RefreshAll(context.Background())

	got := snaps.Repo.Load()
	if got.State != Fresh {
		t.Errorf("state = %v, want Fresh (untouched)", got.State)
	}
	if got.Value.Stars != 5 {
		t.Errorf("Stars = %d, want 5", got.Value.Stars)
	}
}

func TestPoller_RunStopsOnContextCancel(t *testing.T) {
	src := &fakeSource{repo: github.Repo{Stars: 1}}
	snaps := NewSnapshots()
	p := NewPoller(src, snaps, Intervals{
		Repo: 10 * time.Millisecond, Releases: 10 * time.Millisecond,
		Issues: 10 * time.Millisecond, PullRequests: 10 * time.Millisecond,
		Contributors: 10 * time.Millisecond, Stats: 10 * time.Millisecond,
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of context cancellation")
	}
}

func TestIntervals_Defaults(t *testing.T) {
	iv := Intervals{}.withDefaults()
	if iv.Issues != 2*time.Minute {
		t.Errorf("Issues = %v, want 2m", iv.Issues)
	}
	if iv.Stats != 30*time.Minute {
		t.Errorf("Stats = %v, want 30m", iv.Stats)
	}
	// An explicit value must survive.
	if got := (Intervals{Issues: time.Second}).withDefaults().Issues; got != time.Second {
		t.Errorf("explicit Issues = %v, want 1s", got)
	}
}

func TestIntervals_UnauthenticatedDefaultsAreConservative(t *testing.T) {
	iv := (Intervals{Unauthenticated: true}).withDefaults()
	if iv.Issues != 8*time.Minute || iv.PullRequests != 8*time.Minute {
		t.Errorf("unauthenticated intervals = %+v", iv)
	}
}

func TestIntervals_UnauthenticatedKeepsExplicitOverride(t *testing.T) {
	if got := (Intervals{Unauthenticated: true, Issues: time.Minute}).withDefaults().Issues; got != time.Minute {
		t.Errorf("Issues = %v, want explicit 1m", got)
	}
}
