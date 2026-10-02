package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldebello/wt/internal/testutil"
)

func TestCloneCommand(t *testing.T) {
	env := testutil.Setup(t)
	up := env.NewUpstream(t, "domino", "main")

	r := mustRun(t, env, nil, "clone", up)
	if !strings.Contains(r.out, "Repository domino ready") {
		t.Errorf("unexpected output:\n%s", r.out)
	}
	for _, p := range []string{".repos/domino.git", ".repos/domino/README.md"} {
		if _, err := os.Stat(filepath.Join(env.Home, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}

	r = run(t, env, nil, "clone", up)
	if r.e == nil || !strings.Contains(r.e.Error(), "already exists") {
		t.Errorf("expected collision error, got %v", r.e)
	}
}

func TestCloneUsesConfiguredReposPath(t *testing.T) {
	env := testutil.Setup(t)
	up := env.NewUpstream(t, "app", "main")
	custom := filepath.Join(env.Root, "custom-repos")
	testutil.WriteFile(t, filepath.Join(env.WTHome, "settings.toml"), "[paths]\nrepos = \""+custom+"\"\n")

	mustRun(t, env, nil, "clone", up)
	if _, err := os.Stat(filepath.Join(custom, "app.git")); err != nil {
		t.Errorf("clone did not use configured path: %v", err)
	}
}
