package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldebello/wt/internal/testutil"
)

func TestHarnessFollowsWorkspaceLifecycle(t *testing.T) {
	env := setupRepos(t, "api", "web")

	mustRun(t, env, nil, "ws", "W", "--repos", "api,web")
	claude := wsPath(env, "W", "CLAUDE.md")
	data, err := os.ReadFile(claude)
	if err != nil || !strings.Contains(string(data), "`api/`") || !strings.Contains(string(data), "`web/`") {
		t.Fatalf("CLAUDE.md not generated: %v\n%s", err, data)
	}

	// Partial removal regenerates the file.
	mustRun(t, env, nil, "ws", "remove", "W", "--repos", "web", "--yes")
	if data, _ = os.ReadFile(claude); strings.Contains(string(data), "`web/`") {
		t.Errorf("CLAUDE.md not regenerated:\n%s", data)
	}

	// Full removal deletes the generated file and the directory.
	r := mustRun(t, env, nil, "ws", "remove", "W", "--yes")
	if !strings.Contains(r.out, "Removed workspace W") {
		t.Errorf("output:\n%s", r.out)
	}

	// A user-owned file keeps the directory around.
	mustRun(t, env, nil, "ws", "V", "--repos", "api")
	testutil.WriteFile(t, wsPath(env, "V", "CLAUDE.md"), "mine")
	r = mustRun(t, env, nil, "ws", "remove", "V", "--yes")
	if !strings.Contains(r.out, "still contains CLAUDE.md") {
		t.Errorf("output:\n%s", r.out)
	}
}

func TestIntegrationsCommand(t *testing.T) {
	env := setupRepos(t, "api", "web")
	t.Setenv("PATH", "/usr/bin:/bin") // no codegraph

	r := mustRun(t, env, nil, "integrations")
	for _, want := range []string{"codegraph        disabled", "harness claude   enabled   CLAUDE.md", "harness generic  disabled  AGENTS.md"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("list missing %q:\n%s", want, r.out)
		}
	}

	r = run(t, env, nil, "integrations", "codegraph")
	if r.e == nil || !strings.Contains(r.e.Error(), "not enabled") {
		t.Errorf("expected missing binary error, got %v", r.e)
	}

	log := filepath.Join(env.Root, "codegraph.log")
	testutil.StubBinary(t, "codegraph", `echo "$1" >> `+log+`; [ "$1" = init ] && /bin/mkdir -p "$3/.codegraph"; exit 0`)
	mustRun(t, env, nil, "integrations", "codegraph")
	mustRun(t, env, nil, "integrations", "harness", "generic")
	mustRun(t, env, nil, "integrations", "harness", "claude", "--disable")

	settings, _ := os.ReadFile(filepath.Join(env.WTHome, "settings.toml"))
	if !strings.Contains(string(settings), "[integrations.codegraph]\n    enabled = true") {
		t.Errorf("settings.toml:\n%s", settings)
	}

	mustRun(t, env, nil, "ws", "W", "--repos", "api")
	mustRun(t, env, nil, "ws", "W", "--repos", "web")
	mustRun(t, env, nil, "ws", "W", "--repos", "web") // nothing changed: no hooks
	if data, _ := os.ReadFile(log); strings.TrimSpace(string(data)) != "init\nsync" {
		t.Errorf("codegraph calls:\n%s", data)
	}
	if _, err := os.Stat(wsPath(env, "W", "AGENTS.md")); err != nil {
		t.Error("AGENTS.md missing")
	}
	if _, err := os.Stat(wsPath(env, "W", "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("CLAUDE.md generated while disabled")
	}

	r = mustRun(t, env, nil, "ws", "remove", "W", "--yes")
	if !strings.Contains(r.out, "Removed workspace W") {
		t.Errorf("generated files left behind:\n%s", r.out)
	}

	if r := run(t, env, nil, "integrations", "harness", "cursor"); r.e == nil || !strings.Contains(r.e.Error(), "file =") {
		t.Errorf("custom harness without --file: %v", r.e)
	}
	mustRun(t, env, nil, "integrations", "harness", "cursor", "--file", "RULES.md")
}
