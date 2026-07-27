# hushield.com

The marketing site for [HuShield](https://github.com/Hushield/hushield) — a community-driven,
privacy-first spam call and text filter for iOS.

Its primary job is not acquiring app users. It is **recruiting contributors**, which is why the
"Contribute" call to action carries the same visual weight as "See what it does", and why
`/features` shows the gaps alongside the wins.

## Design

**Handlers never call the GitHub API.** A background poller refreshes each dataset on its own
interval and publishes immutable snapshots via `atomic.Pointer`; handlers do a single atomic load.
Three properties follow:

1. Page loads never block on GitHub — latency is template rendering only.
2. Visitor traffic cannot consume rate limit; polling cost is fixed regardless of traffic.
3. Graceful degradation is the default: a failed refresh leaves the previous snapshot serving.

**Every dataset carries a tri-state** — `Fresh`, `Stale(since)`, `Unavailable` — so a failed fetch
and a genuinely empty result can never render identically. An empty leaderboard because nobody has
contributed is an invitation; an empty leaderboard because GitHub returned 500 is a bug being
hidden. `internal/handlers` has tests asserting the two render differently.

**No third-party requests.** No CDN, no web fonts, no analytics, no trackers. A site whose product
claim is "we hold no personal data" should not phone home for a typeface. The CSP in
`deploy/nginx-hushield-web.conf` enforces this.

**Zero dependencies.** Standard library only.

## Layout

```
cmd/web/              entry point, graceful shutdown
internal/github/      typed read-only API client, ETag-aware
internal/cache/       tri-state snapshot store + background poller
internal/catalog/      the feature system (repo-local, not GitHub-derived)
internal/views/        GitHub data -> template shapes; leaderboard ranking
internal/handlers/     one handler per page + a minimal Markdown renderer
internal/templates/    html/template pages
internal/static/       stylesheet and icon
deploy/                systemd unit + nginx vhost
```

The feature catalog is typed Go rather than YAML so a malformed entry is a compile error and the
site keeps its zero-dependency property. `catalog.Validate()` runs at startup and refuses to boot on
a bad entry — including a `help-wanted` feature with no issue to claim, which would be a dead end
for a visitor.

## The leaderboard

Ranked by **merged pull requests**, then commits. Lines changed is displayed as a per-contributor
stat but is deliberately **never** the sort key: ranking on volume rewards vendored dependencies,
generated code, and reformatting commits over a twelve-line fix to a race condition. There is a
test that fails if someone changes this.

`LEADERBOARD_EXCLUDE` drops maintainers from the ranking so newcomers are not permanently measured
against the project owner, and a "new contributors this month" section runs above the table — it is
the strongest available evidence that an outsider's PR actually gets merged.

## Configuration

| Variable | Default | Notes |
|---|---|---|
| `ADDR` | `127.0.0.1:8081` | Loopback only; nginx terminates TLS in front |
| `GITHUB_OWNER` | `Hushield` | |
| `GITHUB_REPO` | `hushield` | |
| `GITHUB_TOKEN` | *(empty)* | Optional but recommended. Unauthenticated is 60 req/hr; authenticated is 5,000. Use a **fine-grained, public-repo, read-only** token — nothing more |
| `LEADERBOARD_EXCLUDE` | *(empty)* | Comma-separated logins omitted from ranking |

## Development

```sh
make test    # go test ./... -race -cover — fixture-driven, no live API calls
make run     # local server, borrows your gh CLI token
make deploy  # test -> arm64 build -> ship -> restart -> health check
```

The deploy host is Graviton/arm64. `make build-linux-arm64` asserts the binary architecture with
`file(1)` and fails the build on a mismatch, because the alternative is discovering it as
`exec format error` in production.
