package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReposListAndRemove(t *testing.T) {
	env := setupRepos(t, "api", "web")
	mustRun(t, env, nil, "ws", "W", "--repos", "api")
	mustRun(t, env, nil, "ws", "V", "--repos", "api")

	r := mustRun(t, env, nil, "repos")
	for _, want := range []string{"REPOSITORY DEFAULT WORKSPACES", "api main V, W", "web main -"} {
		if !strings.Contains(squash(r.out), want) {
			t.Errorf("list missing %q:\n%s", want, r.out)
		}
	}

	// In use by workspaces: refused, nothing deleted.
	r = run(t, env, nil, "repos", "remove", "api", "--yes")
	if r.e == nil || !strings.Contains(r.e.Error(), "used by the worktree") {
		t.Errorf("expected in-use error, got %v", r.e)
	}

	// Confirmation is required without --yes.
	if r = run(t, env, nil, "repos", "remove", "web"); r.e == nil || !strings.Contains(r.e.Error(), "--yes") {
		t.Errorf("expected --yes hint, got %v", r.e)
	}
	if r = run(t, env, &fakeUI{confirms: []bool{false}}, "repos", "remove", "web"); r.e == nil {
		t.Error("expected abort")
	}
	r = mustRun(t, env, &fakeUI{confirms: []bool{true}}, "repos", "remove", "web")
	if !strings.Contains(r.out, "Removed repository web") {
		t.Errorf("output:\n%s", r.out)
	}
	for _, p := range []string{".repos/web", ".repos/web.git"} {
		if _, err := os.Stat(filepath.Join(env.Home, p)); !os.IsNotExist(err) {
			t.Errorf("%s still exists", p)
		}
	}

	if got := completions(t, env, "repos", "remove", ""); len(got) != 1 || got[0] != "api" {
		t.Errorf("completion = %v", got)
	}
}
