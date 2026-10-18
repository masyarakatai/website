package cache

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/Hushield/website/internal/github"
)

// Source is the subset of the GitHub client the poller needs. Declaring it here
// rather than depending on the concrete type keeps the poller testable with a
// fake and documents exactly what it touches.
type Source interface {
	Repo(context.Context) (github.Repo, error)
	Releases(context.Context) ([]github.Release, error)
	Issues(context.Context) ([]github.Issue, error)
	PullRequests(context.Context) ([]github.PullRequest, error)
	Contributors(context.Context) ([]github.Contributor, error)
	ContributorStats(context.Context) ([]github.ContributorStats, error)
}

// Snapshots is the full set of cached GitHub datasets. Handlers read from this.
type Snapshots struct {
	Repo         *Store[github.Repo]
	Releases     *Store[[]github.Release]
	Issues       *Store[[]github.Issue]
	PullRequests *Store[[]github.PullRequest]
	Contributors *Store[[]github.Contributor]
	Stats        *Store[[]github.ContributorStats]
}

// NewSnapshots returns a Snapshots with every dataset Unavailable.
func NewSnapshots() *Snapshots {
	return &Snapshots{
		Repo:         NewStore[github.Repo](),
		Releases:     NewStore[[]github.Release](),
		Issues:       NewStore[[]github.Issue](),
		PullRequests: NewStore[[]github.PullRequest](),
		Contributors: NewStore[[]github.Contributor](),
		Stats:        NewStore[[]github.ContributorStats](),
	}
}

// Intervals controls how often each dataset refreshes. Zero values fall back to
// the documented defaults.
type Intervals struct {
	Repo         time.Duration
	Releases     time.Duration
	Issues       time.Duration
	PullRequests time.Duration
	Contributors time.Duration
	Stats        time.Duration
	// Unauthenticated uses safer defaults for GitHub's 60 requests/hour budget.
	Unauthenticated bool
}

func (i Intervals) withDefaults() Intervals {
	def := func(v, d time.Duration) time.Duration {
		if v <= 0 {
			if i.Unauthenticated {
				return d * 4
			}
			return d
		}
		return v
	}
	result := Intervals{
		Repo:         def(i.Repo, 5*time.Minute),
		Releases:     def(i.Releases, 5*time.Minute),
		Issues:       def(i.Issues, 2*time.Minute),
		PullRequests: def(i.PullRequests, 2*time.Minute),
		Contributors: def(i.Contributors, 30*time.Minute),
		Stats:        def(i.Stats, 30*time.Minute),
	}
	return result
}

// Poller refreshes each dataset on its own schedule.
type Poller struct {
	src       Source
	snaps     *Snapshots
	intervals Intervals
	logger    *log.Logger
	now       func() time.Time
}

// NewPoller wires a Poller. A nil logger disables logging; a nil clock uses
// time.Now.
func NewPoller(src Source, snaps *Snapshots, iv Intervals, logger *log.Logger) *Poller {
	return &Poller{
		src:       src,
		snaps:     snaps,
		intervals: iv.withDefaults(),
		logger:    logger,
		now:       time.Now,
	}
}

func (p *Poller) logf(format string, args ...any) {
	if p.logger != nil {
		p.logger.Printf(format, args...)
	}
}

// refresh runs one fetch and routes the outcome to the store.
//
// This is where the three-way distinction is enforced: 304 keeps the data
// fresh, a stats 202 is a retryable non-failure, and anything else demotes to
// stale while preserving what we had.
func refresh[T any](p *Poller, name string, store *Store[T], fetch func(context.Context) (T, error), ctx context.Context) {
	v, err := fetch(ctx)
	switch {
	case err == nil:
		store.Publish(v, p.now())

	case errors.Is(err, github.ErrNotModified):
		store.KeepFresh(p.now())

	case errors.Is(err, github.ErrStatsComputing):
		// Not an error condition: GitHub is building the cache and will serve
		// real data on a later poll. Do not publish an empty result.
		p.logf("cache: %s statistics still computing; will retry", name)

	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// Shutdown or timeout; leave the snapshot untouched.
		return

	default:
		store.Fail(err)
		p.logf("cache: %s refresh failed, serving previous snapshot: %v", name, err)
	}
}

// Run refreshes everything once, then keeps each dataset on its own ticker until
// ctx is cancelled. It blocks, so callers should run it in a goroutine.
func (p *Poller) Run(ctx context.Context) {
	// Prime everything immediately so the first page load has data.
	p.RefreshAll(ctx)

	type job struct {
		name string
		iv   time.Duration
		run  func(context.Context)
	}
	jobs := []job{
		{"repo", p.intervals.Repo, p.refreshRepo},
		{"releases", p.intervals.Releases, p.refreshReleases},
		{"issues", p.intervals.Issues, p.refreshIssues},
		{"pulls", p.intervals.PullRequests, p.refreshPulls},
		{"contributors", p.intervals.Contributors, p.refreshContributors},
		{"stats", p.intervals.Stats, p.refreshStats},
	}

	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			t := time.NewTicker(j.iv)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					j.run(ctx)
				}
			}
		}(j)
	}
	wg.Wait()
}

// RefreshAll fetches every dataset once, concurrently.
func (p *Poller) RefreshAll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, f := range []func(context.Context){
		p.refreshRepo, p.refreshReleases, p.refreshIssues,
		p.refreshPulls, p.refreshContributors, p.refreshStats,
	} {
		wg.Add(1)
		go func(f func(context.Context)) {
			defer wg.Done()
			f(ctx)
		}(f)
	}
	wg.Wait()
	if source, ok := p.src.(interface{ RateLimit() github.RateLimit }); ok {
		budget := source.RateLimit()
		if !budget.Reset.IsZero() {
			p.logf("github: rate limit remaining=%d reset=%s", budget.Remaining, budget.Reset.UTC().Format(time.RFC3339))
		}
	}
}

func (p *Poller) refreshRepo(ctx context.Context) {
	refresh(p, "repo", p.snaps.Repo, p.src.Repo, ctx)
}
func (p *Poller) refreshReleases(ctx context.Context) {
	refresh(p, "releases", p.snaps.Releases, p.src.Releases, ctx)
}
func (p *Poller) refreshIssues(ctx context.Context) {
	refresh(p, "issues", p.snaps.Issues, p.src.Issues, ctx)
}
func (p *Poller) refreshPulls(ctx context.Context) {
	refresh(p, "pulls", p.snaps.PullRequests, p.src.PullRequests, ctx)
}
func (p *Poller) refreshContributors(ctx context.Context) {
	refresh(p, "contributors", p.snaps.Contributors, p.src.Contributors, ctx)
}
func (p *Poller) refreshStats(ctx context.Context) {
	refresh(p, "stats", p.snaps.Stats, p.src.ContributorStats, ctx)
}
