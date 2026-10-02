package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Paths.Repos != "~/.repos" || cfg.Paths.Workspaces != "~/workspaces" {
		t.Errorf("unexpected default paths: %+v", cfg.Paths)
	}
	if !cfg.Integrations.Harness["claude"].Enabled || cfg.Integrations.Codegraph.Enabled {
		t.Errorf("unexpected default integrations: %+v", cfg.Integrations)
	}
}

func TestLoadOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	content := `
[paths]
repos = "/srv/repos"

[editor]
command = "cursor -n"

[integrations.codegraph]
enabled = true

[bundles.backend]
repos = ["domino", "cws@main"]
`
	if err := os.WriteFile(File(dir), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Paths.Repos != "/srv/repos" {
		t.Errorf("repos = %q", cfg.Paths.Repos)
	}
	if cfg.Paths.Workspaces != "~/workspaces" {
		t.Errorf("workspaces default lost: %q", cfg.Paths.Workspaces)
	}
	if cfg.Editor.Command != "cursor -n" || !cfg.Integrations.Codegraph.Enabled {
		t.Errorf("unexpected config: %+v", cfg)
	}
	if got := cfg.Bundles["backend"].Repos; len(got) != 2 || got[1] != "cws@main" {
		t.Errorf("bundle = %v", got)
	}
}

func TestLoadInvalidFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(File(dir), []byte("[paths\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	cfg := Default()
	cfg.Bundles["frontend"] = Bundle{Repos: []string{"web@dev"}}
	cfg.Integrations.Codegraph.Enabled = true
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Integrations.Codegraph.Enabled || got.Bundles["frontend"].Repos[0] != "web@dev" {
		t.Errorf("round trip mismatch: %+v", got)
	}
	if got.Integrations.Harness["generic"].Template != "agents" {
		t.Errorf("harness lost: %+v", got.Integrations.Harness)
	}
}

func TestExpandHome(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	for in, want := range map[string]string{
		"~":           "/home/u",
		"~/.repos":    "/home/u/.repos",
		"/abs/path":   "/abs/path",
		"~other/path": mustAbs(t, "~other/path"),
	} {
		got, err := ExpandHome(in)
		if err != nil || got != want {
			t.Errorf("ExpandHome(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestHomeDir(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	t.Setenv(HomeEnv, "")
	if got, _ := HomeDir(); got != "/home/u/.wt" {
		t.Errorf("HomeDir = %q", got)
	}
	t.Setenv(HomeEnv, "~/custom")
	if got, _ := HomeDir(); got != "/home/u/custom" {
		t.Errorf("HomeDir with WT_HOME = %q", got)
	}
}

func mustAbs(t *testing.T, p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}
