package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// No test in this package makes a live API call. Everything runs against an
// httptest server with recorded-shape payloads.

func TestRepo_DecodesFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/repos/Hushield/hushield" {
			t.Errorf("path = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q, want Bearer tok", got)
		}
		w.Write([]byte(`{"full_name":"Hushield/hushield","stargazers_count":42,"forks_count":3,"open_issues_count":10}`))
	}))
	defer srv.Close()

	repo, err := New("Hushield", "hushield", "tok", WithBaseURL(srv.URL)).Repo(context.Background())
	if err != nil {
		t.Fatalf("Repo: %v", err)
	}
	if repo.Stars != 42 || repo.Forks != 3 || repo.OpenIssues != 10 {
		t.Errorf("got %+v", repo)
	}
}

// GitHub returns pull requests from the issues endpoint. Counting them as
// issues overstates the open-issue count on every page that shows it.
func TestIssues_FiltersOutPullRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[
			{"number":1,"title":"a real issue"},
			{"number":2,"title":"actually a PR","pull_request":{}},
			{"number":3,"title":"another issue"}
		]`))
	}))
	defer srv.Close()

	issues, err := New("o", "r", "", WithBaseURL(srv.URL)).Issues(context.Background())
	if err != nil {
		t.Fatalf("Issues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("len = %d, want 2 (the PR filtered out)", len(issues))
	}
	for _, i := range issues {
		if i.IsPullRequest() {
			t.Errorf("issue #%d is a PR and should have been filtered", i.Number)
		}
	}
}

func TestReleases_ExcludesDrafts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[
			{"tag_name":"v0.2.0","draft":true},
			{"tag_name":"v0.1.0","draft":false,"published_at":"2026-07-27T20:00:00Z"}
		]`))
	}))
	defer srv.Close()

	rel, err := New("o", "r", "", WithBaseURL(srv.URL)).Releases(context.Background())
	if err != nil {
		t.Fatalf("Releases: %v", err)
	}
	if len(rel) != 1 || rel[0].TagName != "v0.1.0" {
		t.Errorf("got %+v, want only the published v0.1.0", rel)
	}
}

// A 304 must surface as ErrNotModified so the cache can keep its data fresh
// rather than treating an empty body as new state.
func TestNotModifiedReturnsSentinel(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("ETag", `W/"abc123"`)
			w.Write([]byte(`{"stargazers_count":5}`))
			return
		}
		// The client must have replayed the ETag.
		if got := r.Header.Get("If-None-Match"); got != `W/"abc123"` {
			t.Errorf("If-None-Match = %q, want the stored ETag", got)
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	c := New("o", "r", "", WithBaseURL(srv.URL))
	if _, err := c.Repo(context.Background()); err != nil {
		t.Fatalf("first Repo: %v", err)
	}
	_, err := c.Repo(context.Background())
	if !errors.Is(err, ErrNotModified) {
		t.Fatalf("second Repo err = %v, want ErrNotModified", err)
	}
}

// /stats/contributors answers 202 with an empty body while GitHub builds its
// cache. Decoding that as success would publish an empty leaderboard.
func TestContributorStats_202IsRetryableNotEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	_, err := New("o", "r", "", WithBaseURL(srv.URL)).ContributorStats(context.Background())
	if !errors.Is(err, ErrStatsComputing) {
		t.Fatalf("err = %v, want ErrStatsComputing", err)
	}
}

func TestNon2xxIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer srv.Close()

	_, err := New("o", "r", "", WithBaseURL(srv.URL)).Repo(context.Background())
	if err == nil {
		t.Fatal("expected an error on 403")
	}
	if !contains(err.Error(), "403") || !contains(err.Error(), "rate limit") {
		t.Errorf("error should name the status and body: %v", err)
	}
}

func TestRateLimitIsRecordedFromEveryResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "17")
		w.Header().Set("X-RateLimit-Reset", "1760000000")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	c := New("o", "r", "", WithBaseURL(srv.URL))
	_, _ = c.Repo(context.Background())
	got := c.RateLimit()
	if got.Remaining != 17 || got.Reset.Unix() != 1760000000 {
		t.Errorf("RateLimit = %+v", got)
	}
}

func TestPullRequest_IsMerged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[
			{"number":1,"state":"closed","merged_at":"2026-07-27T10:00:00Z"},
			{"number":2,"state":"closed","merged_at":null},
			{"number":3,"state":"open","merged_at":null}
		]`))
	}))
	defer srv.Close()

	prs, err := New("o", "r", "", WithBaseURL(srv.URL)).PullRequests(context.Background())
	if err != nil {
		t.Fatalf("PullRequests: %v", err)
	}
	if !prs[0].IsMerged() {
		t.Error("#1 has merged_at set and should report merged")
	}
	if prs[1].IsMerged() {
		t.Error("#2 was closed without merging and must not report merged")
	}
	if prs[2].IsMerged() {
		t.Error("#3 is open and must not report merged")
	}
}

func TestContributorStats_LinesAndFirstCommit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[{"author":{"login":"x"},"total":9,"weeks":[
			{"w":1750000000,"a":0,"d":0,"c":0},
			{"w":1750604800,"a":100,"d":40,"c":3},
			{"w":1751209600,"a":10,"d":5,"c":6}
		]}]`))
	}))
	defer srv.Close()

	stats, err := New("o", "r", "", WithBaseURL(srv.URL)).ContributorStats(context.Background())
	if err != nil {
		t.Fatalf("ContributorStats: %v", err)
	}
	added, deleted := stats[0].Lines()
	if added != 110 || deleted != 45 {
		t.Errorf("Lines = +%d/-%d, want +110/-45", added, deleted)
	}
	// The first week with zero commits must not count as the first commit.
	if got := stats[0].FirstCommit().Unix(); got != 1750604800 {
		t.Errorf("FirstCommit = %d, want the first week with commits (1750604800)", got)
	}
}

func TestIssue_HasLabel(t *testing.T) {
	i := Issue{Labels: []Label{{Name: "bug"}, {Name: "good-first-issue"}}}
	if !i.HasLabel("good-first-issue") {
		t.Error("HasLabel missed a present label")
	}
	if i.HasLabel("help-wanted") {
		t.Error("HasLabel reported an absent label")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
