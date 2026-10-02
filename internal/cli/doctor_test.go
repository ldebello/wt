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
	testutil.StubBinary(t, "code", "exit 0")
	t.Setenv(shellInitEnv, "zsh")
	mustRun(t, env, nil, "ws", "W", "--repos", "api")

	r := mustRun(t, env, nil, "doctor")
	for _, want := range []string{"[ok] git", "[ok] repo api healthy", "[ok] workspaces 1 workspaces", "[ok] editor", "[ok] shell"} {
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
