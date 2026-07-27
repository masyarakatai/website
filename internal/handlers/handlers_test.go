package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hushield/website/internal/cache"
	"github.com/Hushield/website/internal/config"
	"github.com/Hushield/website/internal/github"
)

var testNow = time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)

func newTestServer(t *testing.T, snaps *cache.Snapshots) http.Handler {
	t.Helper()
	cfg := config.Config{Owner: "Hushield", Repo: "hushield"}
	s, err := New(cfg, snaps, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.now = func() time.Time { return testNow }
	return s.Routes()
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// populated returns snapshots with realistic fresh data.
func populated() *cache.Snapshots {
	s := cache.NewSnapshots()
	s.Repo.Publish(github.Repo{FullName: "Hushield/hushield", Stars: 42, Forks: 3}, testNow)
	s.Releases.Publish([]github.Release{{
		TagName: "v0.1.0", Name: "v0.1.0 — First public release",
		Body:        "## What works\n\n- Backend **shipped**\n- See `internal/scoring`\n- [Docs](https://example.com)\n",
		PublishedAt: testNow.Add(-24 * time.Hour),
		HTMLURL:     "https://github.com/Hushield/hushield/releases/tag/v0.1.0",
	}}, testNow)
	s.Issues.Publish([]github.Issue{
		{Number: 1, Title: "Dockerfile never built", HTMLURL: "u1",
			Labels: []github.Label{{Name: "good-first-issue"}}, CreatedAt: testNow.Add(-48 * time.Hour)},
		{Number: 8, Title: "Real App Attest unverified", HTMLURL: "u8",
			Labels: []github.Label{{Name: "help-wanted"}}, CreatedAt: testNow.Add(-72 * time.Hour)},
	}, testNow)
	s.PullRequests.Publish([]github.PullRequest{
		{Number: 11, Title: "Fix decay rounding", HTMLURL: "p11",
			User: github.User{Login: "outsider"}, MergedAt: &testNow},
	}, testNow)
	s.Contributors.Publish([]github.Contributor{
		{User: github.User{Login: "outsider", AvatarURL: "a", HTMLURL: "h"}, Contributions: 5},
	}, testNow)
	s.Stats.Publish([]github.ContributorStats{
		{Author: github.User{Login: "outsider"}},
	}, testNow)
	return s
}

func TestAllPagesReturn200(t *testing.T) {
	h := newTestServer(t, populated())
	for _, path := range []string{"/", "/features", "/changelog", "/contributors", "/contribute", "/healthz"} {
		rec := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
	}
}

func TestStaticAssetsServed(t *testing.T) {
	h := newTestServer(t, populated())
	rec := get(t, h, "/static/style.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /static/style.css = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "--accent") {
		t.Error("stylesheet body does not look like the real CSS")
	}
}

func TestHome_RendersLiveStats(t *testing.T) {
	h := newTestServer(t, populated())
	body := get(t, h, "/").Body.String()

	for _, want := range []string{"42", "Merged PRs", "Good first issues", "HuShield"} {
		if !strings.Contains(body, want) {
			t.Errorf("home page missing %q", want)
		}
	}
	// No PII claim must be prominent -- it is the product's whole premise.
	if !strings.Contains(body, "never sent anywhere") {
		t.Error("home page does not state the privacy claim")
	}
}

// The central rendering guarantee: an unavailable dataset must SAY so, not
// silently render zeros. Losing this makes a broken integration look like an
// abandoned project.
func TestUnavailableRendersNoticeNotSilentZeros(t *testing.T) {
	empty := cache.NewSnapshots() // nothing ever fetched
	h := newTestServer(t, empty)

	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, "temporarily unavailable") {
		t.Error("home page with no data does not disclose that live data is unavailable")
	}
	if !strings.Contains(body, "not an empty project") {
		t.Error("unavailable notice should distinguish a broken fetch from an empty project")
	}
}

// Stale must render differently from both fresh and unavailable, and must
// disclose the age of what it is showing.
func TestStaleRendersDistinctNotice(t *testing.T) {
	s := populated()
	s.Repo.Fail(errors.New("github: 503 service unavailable"))
	h := newTestServer(t, s)

	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, "last known data") {
		t.Error("stale dataset does not disclose that data is not current")
	}
	if strings.Contains(body, "temporarily unavailable") {
		t.Error("stale data rendered the unavailable notice; the two states must be distinguishable")
	}
	// The previous value must still be shown.
	if !strings.Contains(body, "42") {
		t.Error("stale page dropped the previously-cached star count")
	}
}

func TestFeatures_ShowsStatusesAndClaimLinks(t *testing.T) {
	h := newTestServer(t, populated())
	body := get(t, h, "/features").Body.String()

	for _, want := range []string{"Shipped", "Help wanted", "Claim issue #", "REQ-01"} {
		if !strings.Contains(body, want) {
			t.Errorf("features page missing %q", want)
		}
	}
	if !strings.Contains(body, "github.com/Hushield/hushield/issues/") {
		t.Error("help-wanted features do not link to real issues")
	}
}

func TestChangelog_RendersMarkdown(t *testing.T) {
	h := newTestServer(t, populated())
	body := get(t, h, "/changelog").Body.String()

	if !strings.Contains(body, "<h3>What works</h3>") {
		t.Error("release heading not converted to HTML")
	}
	if !strings.Contains(body, "<strong>shipped</strong>") {
		t.Error("bold markdown not converted")
	}
	if !strings.Contains(body, "<code>internal/scoring</code>") {
		t.Error("code span not converted")
	}
	if !strings.Contains(body, `<a href="https://example.com">Docs</a>`) {
		t.Error("link not converted")
	}
}

func TestChangelog_EmptyStateIsHonest(t *testing.T) {
	s := cache.NewSnapshots()
	s.Releases.Publish(nil, testNow) // a real, successful, empty result
	h := newTestServer(t, s)

	body := get(t, h, "/changelog").Body.String()
	if !strings.Contains(body, "No releases published yet") {
		t.Error("genuinely-empty changelog should say so plainly")
	}
	if strings.Contains(body, "temporarily unavailable") {
		t.Error("a successful empty result must not render as unavailable")
	}
}

func TestContributors_LeaderboardRendersMergedPRColumn(t *testing.T) {
	h := newTestServer(t, populated())
	body := get(t, h, "/contributors").Body.String()

	if !strings.Contains(body, "Merged PRs") {
		t.Error("leaderboard is missing the Merged PRs column")
	}
	if !strings.Contains(body, "outsider") {
		t.Error("contributor row missing")
	}
	// Lines shown as a stat, and the page should explain why it is not the rank.
	if !strings.Contains(body, "Lines changed") {
		t.Error("lines-changed stat missing")
	}
	if !strings.Contains(body, "race condition") {
		t.Error("page does not explain why lines are not the ranking key")
	}
}

func TestContribute_ShowsIssuesAndTheNoPIIRule(t *testing.T) {
	h := newTestServer(t, populated())
	body := get(t, h, "/contribute").Body.String()

	if !strings.Contains(body, "Dockerfile never built") {
		t.Error("good-first-issue not listed")
	}
	if !strings.Contains(body, "Real App Attest unverified") {
		t.Error("help-wanted issue not listed")
	}
	if !strings.Contains(body, "no personally identifiable information") {
		t.Error("contribute page must lead with the no-PII constraint")
	}
	if !strings.Contains(body, "xcodegen generate") {
		t.Error("generated-xcodeproj warning missing from setup instructions")
	}
	if !strings.Contains(body, "Recently merged") {
		t.Error("recently-merged section missing; it is the evidence outsiders get merged")
	}
}

// Release notes are maintainer-authored, but the renderer must still be safe --
// a project whose pitch is "read the code" cannot ship an XSS hole.
func TestRenderMarkdown_EscapesHTML(t *testing.T) {
	got := string(renderMarkdown(`<script>alert(1)</script>`))
	if strings.Contains(got, "<script>") {
		t.Errorf("raw script tag survived escaping: %q", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("expected escaped output, got %q", got)
	}
}

func TestRenderMarkdown_RejectsDangerousLinkSchemes(t *testing.T) {
	got := string(renderMarkdown(`[click](javascript:alert(1))`))
	if strings.Contains(got, "javascript:") {
		t.Errorf("javascript: URL became a live link: %q", got)
	}
	if !strings.Contains(got, "click") {
		t.Errorf("link text should still render as plain text, got %q", got)
	}
}

func TestRenderMarkdown_UnmatchedDelimiterIsLiteral(t *testing.T) {
	got := string(renderMarkdown("a `b c"))
	if strings.Contains(got, "<code>") {
		t.Errorf("unmatched backtick produced a code tag: %q", got)
	}
}

func TestRenderMarkdown_Lists(t *testing.T) {
	got := string(renderMarkdown("- one\n- two\n\nafter"))
	if !strings.Contains(got, "<ul><li>one</li><li>two</li></ul>") {
		t.Errorf("list not rendered correctly: %q", got)
	}
	if !strings.Contains(got, "<p>after</p>") {
		t.Errorf("paragraph after list not rendered: %q", got)
	}
}
