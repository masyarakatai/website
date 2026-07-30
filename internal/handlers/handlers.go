// Package handlers serves the site's pages.
//
// Handlers read only from cache snapshots -- they never call GitHub. See the
// cache package for why.
package handlers

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Hushield/website/internal/cache"
	"github.com/Hushield/website/internal/catalog"
	"github.com/Hushield/website/internal/config"
	"github.com/Hushield/website/internal/static"
	"github.com/Hushield/website/internal/templates"
	"github.com/Hushield/website/internal/views"
)

// Server holds everything the handlers need.
type Server struct {
	cfg   config.Config
	snaps *cache.Snapshots
	tpl   map[string]*template.Template
	log   *log.Logger
	now   func() time.Time
}

// New builds a Server, parsing every template once at startup so a broken
// template fails the process rather than a single request.
func New(cfg config.Config, snaps *cache.Snapshots, logger *log.Logger) (*Server, error) {
	s := &Server{cfg: cfg, snaps: snaps, log: logger, now: time.Now, tpl: map[string]*template.Template{}}

	funcs := template.FuncMap{
		"add": func(a, b int) int { return a + b },
	}

	for _, page := range []string{"home", "features", "changelog", "contributors", "contribute"} {
		t, err := template.New("base.gohtml").Funcs(funcs).ParseFS(templates.FS,
			"base.gohtml", page+".gohtml")
		if err != nil {
			return nil, fmt.Errorf("handlers: parse %s: %w", page, err)
		}
		s.tpl[page] = t
	}
	return s, nil
}

// Routes returns the site's mux.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /static/", http.StripPrefix("/static/",
		cacheControl(http.FileServer(http.FS(static.FS)), "public, max-age=3600")))

	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /features", s.features)
	mux.HandleFunc("GET /changelog", s.changelog)
	mux.HandleFunc("GET /contributors", s.contributors)
	mux.HandleFunc("GET /contribute", s.contribute)
	return mux
}

func cacheControl(h http.Handler, v string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", v)
		h.ServeHTTP(w, r)
	})
}

func (s *Server) render(w http.ResponseWriter, page string, data any) {
	t, ok := s.tpl[page]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		// A response may already be partially written, so this cannot become a
		// clean 500. Log it loudly instead of pretending it succeeded.
		if s.log != nil {
			s.log.Printf("handlers: render %s: %v", page, err)
		}
	}
}

type pageMeta struct {
	Title       string
	Description string
	Nav         string
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status":"ok"}`)
}

// ---- home ----

type homeData struct {
	pageMeta
	Stats views.Stats
}

func (s *Server) home(w http.ResponseWriter, _ *http.Request) {
	repo := s.snaps.Repo.Load()
	contribs := s.snaps.Contributors.Load()
	prs := s.snaps.PullRequests.Load()
	issues := s.snaps.Issues.Load()

	merged := 0
	for _, p := range prs.Value {
		if p.IsMerged() {
			merged++
		}
	}
	goodFirst := 0
	for _, i := range issues.Value {
		if i.HasLabel("good-first-issue") {
			goodFirst++
		}
	}

	st := views.Stats{
		Stars:        repo.Value.Stars,
		Forks:        repo.Value.Forks,
		OpenIssues:   len(issues.Value),
		Contributors: len(contribs.Value),
		MergedPRs:    merged,
		GoodFirst:    goodFirst,
		Freshness:    views.FreshnessOf(repo),
	}

	s.render(w, "home", homeData{
		pageMeta: pageMeta{
			Title:       "Community spam call and text blocking for iPhone",
			Description: "A free, open-source spam call and text filter for iPhone that holds no personal data. No accounts, no contact upload.",
			Nav:         "home",
		},
		Stats: st,
	})
}

// ---- features ----

type featureVM struct {
	Title       string
	Summary     string
	Status      string
	StatusLabel string
	Requirement string
	Issue       int
	IssueURL    string
}

type featureGroup struct {
	AreaLabel string
	Features  []featureVM
}

type featuresData struct {
	pageMeta
	Groups          []featureGroup
	HelpWantedCount int
}

func (s *Server) features(w http.ResponseWriter, _ *http.Request) {
	// The catalog's status is hand-maintained, so a help-wanted entry can outlive
	// the issue it points at -- which means advertising claimable work that is
	// already done, and sending a would-be contributor to a closed issue.
	//
	// Cross-check against the live issue snapshot instead of trusting the file.
	// Only when the snapshot is usable: if GitHub is unreachable we know nothing
	// about issue state and must not silently reclassify the whole roadmap.
	issues := s.snaps.Issues.Load()
	openIssues := map[int]bool{}
	if issues.OK() {
		for _, i := range issues.Value {
			openIssues[i.Number] = true
		}
	}
	trustIssueState := issues.OK() && len(openIssues) > 0

	order := []catalog.Area{catalog.AreaIOS, catalog.AreaBackend, catalog.AreaOps}
	groups := make([]featureGroup, 0, len(order))

	for _, area := range order {
		var fs []featureVM
		for _, f := range catalog.Features {
			if f.Area != area {
				continue
			}
			status := f.Status
			// A help-wanted feature whose issue is no longer open is finished
			// work the catalog has not caught up with. Show it as shipped and
			// drop the claim link rather than inviting a click into a closed issue.
			staleClaim := trustIssueState &&
				f.Status == catalog.HelpWanted &&
				f.Issue > 0 &&
				!openIssues[f.Issue]
			if staleClaim {
				status = catalog.Shipped
			}

			vm := featureVM{
				Title: f.Title, Summary: f.Summary,
				Status: string(status), StatusLabel: status.Label(),
				Requirement: f.Requirement, Issue: f.Issue,
			}
			if f.Issue > 0 && !staleClaim {
				vm.IssueURL = fmt.Sprintf("https://github.com/%s/%s/issues/%d",
					s.cfg.Owner, s.cfg.Repo, f.Issue)
			}
			fs = append(fs, vm)
		}
		if len(fs) > 0 {
			groups = append(groups, featureGroup{AreaLabel: area.Label(), Features: fs})
		}
	}

	// Count what a visitor can actually claim right now.
	claimable := 0
	for _, g := range groups {
		for _, f := range g.Features {
			if f.Status == string(catalog.HelpWanted) {
				claimable++
			}
		}
	}

	s.render(w, "features", featuresData{
		pageMeta: pageMeta{
			Title:       "Features",
			Description: "Everything HuShield does, with honest status on what is verified and what is not.",
			Nav:         "features",
		},
		Groups:          groups,
		HelpWantedCount: claimable,
	})
}

// ---- changelog ----

type releaseVM struct {
	Name           string
	Tag            string
	URL            string
	PublishedISO   string
	PublishedHuman string
	Prerelease     bool
	BodyHTML       template.HTML
}

type changelogData struct {
	pageMeta
	Releases  []releaseVM
	Freshness views.Freshness
}

func (s *Server) changelog(w http.ResponseWriter, _ *http.Request) {
	rel := s.snaps.Releases.Load()
	out := make([]releaseVM, 0, len(rel.Value))
	for _, r := range rel.Value {
		name := r.Name
		if name == "" {
			name = r.TagName
		}
		out = append(out, releaseVM{
			Name: name, Tag: r.TagName, URL: r.HTMLURL,
			PublishedISO:   r.PublishedAt.Format(time.RFC3339),
			PublishedHuman: r.PublishedAt.Format("2 January 2006"),
			Prerelease:     r.Prerelease,
			BodyHTML:       renderMarkdown(r.Body),
		})
	}

	s.render(w, "changelog", changelogData{
		pageMeta: pageMeta{
			Title:       "Changelog",
			Description: "Every HuShield release, pulled live from GitHub.",
			Nav:         "changelog",
		},
		Releases:  out,
		Freshness: views.FreshnessOf(rel),
	})
}

// ---- contributors ----

type contributorsData struct {
	pageMeta
	Leaderboard views.Leaderboard
	Freshness   views.Freshness
}

func (s *Server) contributors(w http.ResponseWriter, _ *http.Request) {
	contribs := s.snaps.Contributors.Load()
	stats := s.snaps.Stats.Load()
	prs := s.snaps.PullRequests.Load()

	board := views.BuildLeaderboard(
		contribs.Value, stats.Value, prs.Value,
		s.cfg.ExcludeFromLeaderboard, s.now(),
	)
	board.Freshness = views.FreshnessOf(contribs)

	s.render(w, "contributors", contributorsData{
		pageMeta: pageMeta{
			Title:       "Contributors",
			Description: "Everyone building HuShield, ranked by merged pull requests.",
			Nav:         "contributors",
		},
		Leaderboard: board,
		Freshness:   views.FreshnessOf(contribs),
	})
}

// ---- contribute ----

type contributeData struct {
	pageMeta
	GoodFirst    []views.IssueRow
	HelpWanted   []views.IssueRow
	RecentMerged []views.PRRow
	Freshness    views.Freshness
}

func (s *Server) contribute(w http.ResponseWriter, _ *http.Request) {
	issues := s.snaps.Issues.Load()
	prs := s.snaps.PullRequests.Load()

	gf, hw, _, _ := views.PartitionIssues(issues.Value, s.now())

	s.render(w, "contribute", contributeData{
		pageMeta: pageMeta{
			Title:       "Contribute",
			Description: "Real, claimable work on HuShield, with setup instructions and the one design rule that matters.",
			Nav:         "contribute",
		},
		GoodFirst:    gf,
		HelpWanted:   hw,
		RecentMerged: views.RecentMergedPRs(prs.Value, 5, s.now()),
		Freshness:    views.FreshnessOf(issues),
	})
}

// renderMarkdown converts the small subset of Markdown that appears in release
// notes into HTML: headings, bullets, code spans, links, bold, and paragraphs.
//
// Deliberately not a full Markdown library. Release notes are authored by
// maintainers, the output is escaped before any inline formatting is applied,
// and a dependency-free site is easier to audit -- which matters for a project
// whose whole claim is that you can read it yourself.
func renderMarkdown(src string) template.HTML {
	var b strings.Builder
	inList := false

	closeList := func() {
		if inList {
			b.WriteString("</ul>")
			inList = false
		}
	}

	for _, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			closeList()
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "### "):
			closeList()
			b.WriteString("<h4>" + inlineMD(trimmed[4:]) + "</h4>")
		case strings.HasPrefix(trimmed, "## "):
			closeList()
			b.WriteString("<h3>" + inlineMD(trimmed[3:]) + "</h3>")
		case strings.HasPrefix(trimmed, "# "):
			closeList()
			b.WriteString("<h3>" + inlineMD(trimmed[2:]) + "</h3>")
		case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "):
			if !inList {
				b.WriteString("<ul>")
				inList = true
			}
			b.WriteString("<li>" + inlineMD(trimmed[2:]) + "</li>")
		default:
			closeList()
			b.WriteString("<p>" + inlineMD(trimmed) + "</p>")
		}
	}
	closeList()
	return template.HTML(b.String())
}

// inlineMD escapes the input first, then applies inline formatting. Escaping
// before formatting is what keeps this safe: no untrusted substring can become
// markup.
func inlineMD(s string) string {
	out := template.HTMLEscapeString(s)
	out = wrapPairs(out, "`", "<code>", "</code>")
	out = wrapPairs(out, "**", "<strong>", "</strong>")
	out = linkify(out)
	return out
}

// wrapPairs replaces balanced pairs of a delimiter with open/close tags. An
// unmatched trailing delimiter is left as literal text.
func wrapPairs(s, delim, open, close string) string {
	var b strings.Builder
	rest := s
	for {
		i := strings.Index(rest, delim)
		if i < 0 {
			b.WriteString(rest)
			return b.String()
		}
		j := strings.Index(rest[i+len(delim):], delim)
		if j < 0 {
			b.WriteString(rest)
			return b.String()
		}
		b.WriteString(rest[:i])
		b.WriteString(open)
		b.WriteString(rest[i+len(delim) : i+len(delim)+j])
		b.WriteString(close)
		rest = rest[i+len(delim)+j+len(delim):]
	}
}

// linkify turns [text](url) into an anchor. Only http(s) and root-relative
// targets are linked; anything else renders as plain text so a javascript: URL
// in release notes cannot become a live link.
func linkify(s string) string {
	var b strings.Builder
	rest := s
	for {
		open := strings.Index(rest, "[")
		if open < 0 {
			b.WriteString(rest)
			return b.String()
		}
		close := strings.Index(rest[open:], "](")
		if close < 0 {
			b.WriteString(rest)
			return b.String()
		}
		close += open
		end := strings.Index(rest[close:], ")")
		if end < 0 {
			b.WriteString(rest)
			return b.String()
		}
		end += close

		text := rest[open+1 : close]
		url := rest[close+2 : end]

		b.WriteString(rest[:open])
		if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "/") {
			b.WriteString(`<a href="` + url + `">` + text + `</a>`)
		} else {
			b.WriteString(text)
		}
		rest = rest[end+1:]
	}
}
