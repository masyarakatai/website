package config

import "testing"

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("ADDR", "")
	t.Setenv("GITHUB_OWNER", "")
	t.Setenv("GITHUB_REPO", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("LEADERBOARD_EXCLUDE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != "127.0.0.1:8081" {
		t.Errorf("Addr = %q, want loopback:8081 (nginx terminates TLS in front)", cfg.Addr)
	}
	if cfg.Owner != "Hushield" || cfg.Repo != "hushield" {
		t.Errorf("repo = %s/%s, want Hushield/hushield", cfg.Owner, cfg.Repo)
	}
	if cfg.TokenConfigured() {
		t.Error("TokenConfigured() = true with no token set")
	}
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("ADDR", ":9000")
	t.Setenv("GITHUB_OWNER", "acme")
	t.Setenv("GITHUB_REPO", "widget")
	t.Setenv("GITHUB_TOKEN", "ghp_example")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != ":9000" || cfg.Owner != "acme" || cfg.Repo != "widget" {
		t.Errorf("got %+v", cfg)
	}
	if !cfg.TokenConfigured() {
		t.Error("TokenConfigured() = false with a token set")
	}
}

func TestLoad_ExcludeList(t *testing.T) {
	t.Setenv("LEADERBOARD_EXCLUDE", " jbrahy , bot-account ,, ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.ExcludeFromLeaderboard) != 2 {
		t.Fatalf("got %d entries, want 2 (blanks dropped): %v", len(cfg.ExcludeFromLeaderboard), cfg.ExcludeFromLeaderboard)
	}
	if !cfg.ExcludeFromLeaderboard["jbrahy"] || !cfg.ExcludeFromLeaderboard["bot-account"] {
		t.Errorf("entries not trimmed correctly: %v", cfg.ExcludeFromLeaderboard)
	}
}
