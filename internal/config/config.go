// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config is the site's runtime configuration.
type Config struct {
	Addr        string
	Owner       string
	Repo        string
	GitHubToken string
	// ExcludeFromLeaderboard are logins omitted from the contributor ranking so
	// newcomers are not permanently measured against the maintainer.
	ExcludeFromLeaderboard map[string]bool
	RequestTimeout         time.Duration
}

const (
	defaultAddr  = "127.0.0.1:8081"
	defaultOwner = "Hushield"
	defaultRepo  = "hushield"
)

// Load reads configuration, applying defaults.
//
// GITHUB_TOKEN is optional but strongly recommended: unauthenticated requests
// are capped at 60/hour, below what the poll schedule needs, so an unset token
// means datasets will intermittently go stale.
func Load() (Config, error) {
	cfg := Config{
		Addr:                   env("ADDR", defaultAddr),
		Owner:                  env("GITHUB_OWNER", defaultOwner),
		Repo:                   env("GITHUB_REPO", defaultRepo),
		GitHubToken:            os.Getenv("GITHUB_TOKEN"),
		RequestTimeout:         15 * time.Second,
		ExcludeFromLeaderboard: parseList(env("LEADERBOARD_EXCLUDE", "")),
	}

	if cfg.Owner == "" || cfg.Repo == "" {
		return Config{}, fmt.Errorf("config: GITHUB_OWNER and GITHUB_REPO must be non-empty")
	}
	return cfg, nil
}

// TokenConfigured reports whether an API token is present, so startup can warn
// once rather than failing.
func (c Config) TokenConfigured() bool { return c.GitHubToken != "" }

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func parseList(s string) map[string]bool {
	out := make(map[string]bool)
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out[p] = true
		}
	}
	return out
}
