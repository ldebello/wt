package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldebello/wt/internal/testutil"
)

func TestDoctor(t *testing.T) {
	env := setupRepos(t, "api")
	t.Setenv("PATH", "/usr/bin:/bin") // no codegraph
	testutil.StubBinary(t, "code", "exit 0")
	t.Setenv(shellInitEnv, "zsh")
	mustRun(t, env, nil, "ws", "W", "--repos", "api")

	r := mustRun(t, env, nil, "doctor")
	for _, want := range []string{"[ok] git", "[ok] repo api healthy", "[ok] workspaces 1 workspaces", "[ok] editor", "[ok] shell",
		"[ok] codegraph not installed", "npm install -g @colbymchenry/codegraph", "codegraph telemetry off",
		"[ok] harness generic disabled enable: wt integrations harness generic"} {
		if !strings.Contains(squash(r.out), want) {
			t.Errorf("doctor missing %q:\n%s", want, r.out)
		}
	}

	// Break things that --fix can repair.
	bare := filepath.Join(env.Home, ".repos", "api.git")
	primary := filepath.Join(env.Home, ".repos", "api")
	testutil.Git(t, bare, "remote", "set-head", "origin", "--delete")
	os.RemoveAll(primary)
	t.Setenv(shellInitEnv, "")

	r = run(t, env, nil, "doctor")
	if r.e == nil {
		t.Error("expected problems")
	}
	for _, want := range []string{"origin/HEAD is not set", "primary checkout missing", "stale worktree registration", "shell integration not loaded", "wt doctor --fix"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("doctor missing %q:\n%s", want, r.out)
		}
	}

	r = mustRun(t, env, nil, "doctor", "--fix")
	if !strings.Contains(r.out, "Applied 3 fixes") || !strings.Contains(r.out, "healthy") {
		t.Errorf("fix output:\n%s", r.out)
	}
	if _, err := os.Stat(filepath.Join(primary, "README.md")); err != nil {
		t.Error("primary not restored")
	}

	// A workspace whose repository was deleted is reported.
	os.RemoveAll(bare)
	os.RemoveAll(primary)
	r = run(t, env, nil, "doctor")
	if r.e == nil || !strings.Contains(r.out, "workspace W") || !strings.Contains(r.out, "missing from") {
		t.Errorf("doctor output:\n%s", r.out)
	}
}

func TestDoctorCodegraphStates(t *testing.T) {
	env := testutil.Setup(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	testutil.WriteFile(t, filepath.Join(env.WTHome, "settings.toml"), "[integrations.codegraph]\nenabled = true\n")
	r := run(t, env, nil, "doctor")
	if r.e == nil || !strings.Contains(squash(r.out), "[error] codegraph 'codegraph' was not found in PATH") || !strings.Contains(r.out, "npm install") {
		t.Errorf("enabled but missing: %v\n%s", r.e, r.out)
	}

	testutil.StubBinary(t, "codegraph", "exit 0")
	r = run(t, env, nil, "doctor")
	if !strings.Contains(squash(r.out), "[ok] codegraph enabled") {
		t.Errorf("enabled:\n%s", r.out)
	}
}

func TestDoctorReportsBadSettings(t *testing.T) {
	env := testutil.Setup(t)
	testutil.WriteFile(t, filepath.Join(env.WTHome, "settings.toml"), "[paths\n")
	if r := run(t, env, nil, "doctor"); r.e == nil || !strings.Contains(r.e.Error(), "settings") {
		t.Errorf("got %v", r.e)
	}
}

// squash collapses runs of whitespace so assertions ignore column alignment.
func squash(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
