// Package catalog holds the HuShield feature system.
//
// This data is repo-local and deliberately NOT derived from GitHub: the roadmap
// needs to describe work that has no issue yet, and it should render even when
// the GitHub API is unreachable.
//
// It is typed Go rather than YAML so that a malformed entry is a compile error
// instead of a runtime surprise, and so the site has zero dependencies.
package catalog

// Status is a feature's lifecycle position.
type Status string

const (
	Shipped    Status = "shipped"
	InProgress Status = "in-progress"
	Planned    Status = "planned"
	// HelpWanted marks work explicitly open to outside contributors. Every
	// HelpWanted entry should carry an Issue number so the page can link
	// straight to something claimable.
	HelpWanted Status = "help-wanted"
)

// Label returns the human-readable status.
func (s Status) Label() string {
	switch s {
	case Shipped:
		return "Shipped"
	case InProgress:
		return "In progress"
	case Planned:
		return "Planned"
	case HelpWanted:
		return "Help wanted"
	}
	return string(s)
}

// Area groups features by the part of the system they live in.
type Area string

const (
	AreaBackend Area = "backend"
	AreaIOS     Area = "ios"
	AreaOps     Area = "ops"
)

// Label returns the human-readable area.
func (a Area) Label() string {
	switch a {
	case AreaBackend:
		return "Backend"
	case AreaIOS:
		return "iOS"
	case AreaOps:
		return "Operations"
	}
	return string(a)
}

// Feature is one entry in the catalog.
type Feature struct {
	ID      string
	Title   string
	Summary string
	Status  Status
	Area    Area
	// Requirement traces back to .planning/PROJECT.md (e.g. "REQ-03"), so the
	// site and the internal planning docs cannot drift apart unnoticed.
	Requirement string
	// Issue is the GitHub issue number for claimable work, 0 when none.
	Issue int
}

// Features is the catalog, ordered so the page reads as a narrative: what the
// product does, then how it is operated, then what still needs doing.
//
// Status values reflect verified reality, not intent. Anything that has never
// run against real hardware or a real deployment is not "shipped".
var Features = []Feature{
	{
		ID: "community-reporting", Title: "Community spam reporting",
		Summary: "Any attested device can report a number as spam or not-spam. A device's identity is an Apple App Attest key and nothing else — no account, no phone number, no contact upload.",
		Status:  Shipped, Area: AreaBackend, Requirement: "REQ-01",
	},
	{
		ID: "decaying-score", Title: "Trust-weighted, time-decaying scoring",
		Summary: "Each report contributes trust_weight × category_multiplier × exponential_decay(age, 30-day half-life). Not-spam votes subtract. Crossing 2.0 labels a number; 5.0 blocks it. An admin override always wins.",
		Status:  Shipped, Area: AreaBackend, Requirement: "REQ-02",
	},
	{
		ID: "device-trust", Title: "Per-device reputation",
		Summary: "A device's vote weight is a pure function of its reporting tenure, volume, and how often its votes disagreed with the eventual community outcome. This is what makes brigading expensive.",
		Status:  Shipped, Area: AreaBackend, Requirement: "REQ-02",
	},
	{
		ID: "blocklist-delta", Title: "Efficient blocklist delta",
		Summary: "Keyset pagination, neighbour-spoof widening, crowd-sourced caller ID, and removal tombstones so a number that stops being blockable is explicitly un-blocked on clients rather than silently vanishing from the feed.",
		Status:  Shipped, Area: AreaBackend, Requirement: "REQ-03",
	},
	{
		ID: "cold-start-seed", Title: "Cold-start seeding from public data",
		Summary: "Imports FTC and FCC public complaint datasets via synthetic seed devices, so the blocklist is useful before the first real user arrives.",
		Status:  Shipped, Area: AreaBackend, Requirement: "REQ-04",
	},
	{
		ID: "token-lifecycle", Title: "Device token lifecycle",
		Summary: "attest → verify → assert-refresh, with strictly-increasing counter replay protection. Tokens refresh without re-attesting.",
		Status:  Shipped, Area: AreaBackend, Requirement: "REQ-05",
	},
	{
		ID: "reputation-lookup", Title: "On-demand number lookup",
		Summary: "Look up the current reputation of a single number without pulling the whole blocklist.",
		Status:  Shipped, Area: AreaBackend, Requirement: "REQ-06",
	},
	{
		ID: "silent-push", Title: "Silent push refresh",
		Summary: "Server-initiated APNs silent pushes nudge clients to resync so a newly-blocked number reaches devices without waiting for the next poll.",
		Status:  InProgress, Area: AreaBackend, Requirement: "REQ-07",
	},
	{
		ID: "call-directory", Title: "Call blocking and labelling",
		Summary: "A Call Directory extension blocks known-bad numbers outright and labels suspected ones, driven entirely by the locally-synced list.",
		Status:  InProgress, Area: AreaIOS, Requirement: "REQ-08",
	},
	{
		ID: "sms-filter", Title: "Offline SMS filtering",
		Summary: "An SMS Filter extension classifies incoming messages with no network access at all. Filtering must work in airplane mode.",
		Status:  InProgress, Area: AreaIOS, Requirement: "REQ-09",
	},
	{
		ID: "reporting-ui", Title: "In-app reporting and caller ID",
		Summary: "Report spam or not-spam, look up a number, and contribute a community caller-ID name.",
		Status:  Shipped, Area: AreaIOS, Requirement: "REQ-10",
	},
	{
		ID: "appgroup-sync", Title: "Shared App Group sync",
		Summary: "One framework handles App Attest enrolment, token refresh, and blocklist delta sync, shared across the app and both extensions via an App Group.",
		Status:  Shipped, Area: AreaIOS, Requirement: "REQ-11",
	},
	{
		ID: "prod-hardening", Title: "Production hardening",
		Summary: "Real App Attest validated on physical hardware, a shared challenge store for multi-instance deploys, and scheduled score recomputation.",
		Status:  Shipped, Area: AreaOps, Requirement: "REQ-12",
	},
	{
		ID: "docker-image", Title: "Verified container image",
		Summary: "The Dockerfile has never been built — it was authored without a Docker daemon and sets no GOARCH. Production uses systemd instead. Fixing or removing it are both welcome.",
		Status:  HelpWanted, Area: AreaOps, Issue: 1,
	},
	{
		ID: "prod-smoke", Title: "Production smoke test",
		Summary: "The existing smoke test fabricates attestations, so it only works against a mock-mode server and cannot verify a real deployment.",
		Status:  HelpWanted, Area: AreaOps, Issue: 2,
	},
	{
		ID: "strict-mode", Title: "Strict mode for unknown callers",
		Summary: "Silence callers who aren't in your contacts, but let numbers the community has vouched for ring through anyway \u2014 so you don't lose the pharmacy and the school along with the spam.",
		Status:  HelpWanted, Area: AreaIOS, Issue: 13,
	},
	{
		ID: "ci", Title: "Continuous integration",
		Summary: "There is no CI. The test gate lives in a local Makefile, which means a contributor cannot see their PR pass before a maintainer runs it by hand.",
		Status:  HelpWanted, Area: AreaOps, Issue: 10,
	},
	{
		ID: "real-attest", Title: "Verified real App Attest",
		Summary: "Apple App Attest verified end to end against production from a physical iPhone \u2014 confirmed by the Apple-issued attestation receipt stored server-side, with fabricated attestations correctly rejected.",
		Status:  Shipped, Area: AreaBackend, Requirement: "REQ-05",
	},
}

// ByStatus returns every feature with the given status, preserving order.
func ByStatus(s Status) []Feature {
	out := make([]Feature, 0, len(Features))
	for _, f := range Features {
		if f.Status == s {
			out = append(out, f)
		}
	}
	return out
}

// Counts returns how many features sit in each status.
func Counts() map[Status]int {
	m := make(map[Status]int, 4)
	for _, f := range Features {
		m[f.Status]++
	}
	return m
}

// Validate reports structural problems in the catalog. Called at startup so a
// bad entry fails fast rather than rendering a broken page.
func Validate() []string {
	var problems []string
	seen := make(map[string]bool, len(Features))

	for _, f := range Features {
		switch {
		case f.ID == "":
			problems = append(problems, "feature with empty ID: "+f.Title)
		case seen[f.ID]:
			problems = append(problems, "duplicate feature ID: "+f.ID)
		}
		seen[f.ID] = true

		if f.Title == "" || f.Summary == "" {
			problems = append(problems, f.ID+": title and summary are required")
		}

		switch f.Status {
		case Shipped, InProgress, Planned, HelpWanted:
		default:
			problems = append(problems, f.ID+": unknown status "+string(f.Status))
		}

		switch f.Area {
		case AreaBackend, AreaIOS, AreaOps:
		default:
			problems = append(problems, f.ID+": unknown area "+string(f.Area))
		}

		// A help-wanted entry without an issue is a dead end: it tells someone
		// work exists but gives them nothing to claim.
		if f.Status == HelpWanted && f.Issue == 0 {
			problems = append(problems, f.ID+": help-wanted features must reference a GitHub issue")
		}
	}
	return problems
}
