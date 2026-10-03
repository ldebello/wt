package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldebello/wt/internal/testutil"
)

// setupRepos isolates the environment and clones one upstream per name.
func setupRepos(t *testing.T, names ...string) *testutil.Env {
	t.Helper()
	env := testutil.Setup(t)
	for _, name := range names {
		mustRun(t, env, nil, "clone", env.NewUpstream(t, name, "main"))
	}
	return env
}

func wsPath(env *testutil.Env, parts ...string) string {
	return filepath.Join(append([]string{env.Home, "workspaces"}, parts...)...)
}

func TestWorkspaceLifecycle(t *testing.T) {
	env := setupRepos(t, "api", "web", "tools")

	r := mustRun(t, env, nil, "ws", "DOM-1", "--repos", "api,web@main")
	for _, want := range []string{"Workspace DOM-1", "api", "new branch DOM-1 from origin/main", "web", "branch main tracking origin/main"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("output missing %q:\n%s", want, r.out)
		}
	}

	// Without --repos/--bundles the picker opens, with members preselected.
	// Members stay untouched even if deselected.
	fake := &fakeUI{multi: [][]string{{"repo:tools"}}}
	r = mustRun(t, env, fake, "workspace", "DOM-1")
	if !strings.Contains(r.out, "tools") || strings.Contains(r.out, "api") {
		t.Errorf("unexpected output:\n%s", r.out)
	}
	// Sorted: api, tools, web.
	if opts := fake.offered[0]; len(opts) != 3 || !opts[0].Selected || opts[1].Selected || !opts[2].Selected || opts[0].Value != "repo:api" {
		t.Errorf("picker options: %+v", opts)
	}
	if fake.asked[0] != "Add to workspace DOM-1" {
		t.Errorf("asked %v", fake.asked)
	}

	testutil.WriteFile(t, wsPath(env, "DOM-1", "web", "dirty.txt"), "x")
	r = mustRun(t, env, nil, "ws", "list")
	if !strings.Contains(r.out, "DOM-1") || !strings.Contains(r.out, "api@DOM-1  tools@DOM-1  web@main*") {
		t.Errorf("list output:\n%s", r.out)
	}

	// Partial removal.
	r = mustRun(t, env, nil, "ws", "remove", "DOM-1", "--repos", "tools", "--yes")
	if !strings.Contains(r.out, "tools") || !strings.Contains(r.out, "worktree removed, branch DOM-1 deleted") {
		t.Errorf("remove output:\n%s", r.out)
	}
	if _, err := os.Stat(wsPath(env, "DOM-1", "tools")); !os.IsNotExist(err) {
		t.Error("tools not removed")
	}

	// Full removal keeps the dirty worktree and the directory.
	r = mustRun(t, env, nil, "ws", "remove", "DOM-1", "--yes")
	if !strings.Contains(r.out, "kept: has uncommitted changes") || !strings.Contains(r.out, "some repositories could not be removed") {
		t.Errorf("remove output:\n%s", r.out)
	}
	os.Remove(wsPath(env, "DOM-1", "web", "dirty.txt"))
	r = mustRun(t, env, nil, "ws", "remove", "DOM-1", "--yes")
	if !strings.Contains(r.out, "Removed workspace DOM-1") {
		t.Errorf("remove output:\n%s", r.out)
	}
	if _, err := os.Stat(wsPath(env, "DOM-1")); !os.IsNotExist(err) {
		t.Error("workspace dir still exists")
	}
}

func TestWorkspaceRemoveAsksForConfirmation(t *testing.T) {
	env := setupRepos(t, "api")
	mustRun(t, env, nil, "ws", "W", "--repos", "api")

	r := run(t, env, nil, "ws", "remove", "W")
	if r.e == nil || !strings.Contains(r.e.Error(), "--yes") {
		t.Errorf("expected --yes hint, got %v", r.e)
	}

	fake := &fakeUI{confirms: []bool{false}}
	if r = run(t, env, fake, "ws", "remove", "W"); r.e == nil {
		t.Error("expected abort")
	}

	fake = &fakeUI{confirms: []bool{true, false}}
	r = mustRun(t, env, fake, "ws", "remove", "W")
	if len(fake.asked) != 2 || !strings.Contains(fake.asked[1], "origin") {
		t.Errorf("asked: %v", fake.asked)
	}
}

func TestWorkspaceErrors(t *testing.T) {
	env := setupRepos(t, "api")

	if r := run(t, env, nil, "ws", "list", "--repos", "api"); r.e == nil {
		t.Error("expected error for flags on list")
	}
	if r := run(t, env, nil, "ws", "remove"); r.e == nil {
		t.Error("expected error for missing name")
	}
	r := run(t, env, nil, "ws", "a/b", "--repos", "api")
	if r.e == nil || !strings.Contains(r.e.Error(), "invalid workspace name") {
		t.Errorf("got %v", r.e)
	}
	r = run(t, env, nil, "ws", "W", "--repos", "api,ghost")
	if r.e == nil || !strings.Contains(r.e.Error(), "nothing was created") || !strings.Contains(r.e.Error(), "wt clone") {
		t.Errorf("got %v", r.e)
	}
	if _, err := os.Stat(wsPath(env, "W")); !os.IsNotExist(err) {
		t.Error("workspace created despite errors")
	}
	r = run(t, env, nil, "ws", "W", "--repos")
	if r.e == nil || !strings.Contains(r.e.Error(), "flag needs an argument") {
		t.Errorf("expected missing value error, got %v", r.e)
	}
	r = run(t, env, nil, "ws", "W")
	if r.e == nil || !strings.Contains(r.e.Error(), "pass --repos") {
		t.Errorf("expected no-TTY error, got %v", r.e)
	}

	// Picking nothing creates an empty workspace.
	r = mustRun(t, env, &fakeUI{multi: [][]string{{}}}, "ws", "Empty")
	if !strings.Contains(r.out, "(empty; add repositories") {
		t.Errorf("output:\n%s", r.out)
	}
}

func TestWorkspaceRepeatedRepos(t *testing.T) {
	env := setupRepos(t, "api", "web", "tools")
	mustRun(t, env, nil, "ws", "W", "--repos", "api", "--repos", "web@main,tools")
	r := mustRun(t, env, nil, "ws", "list")
	if !strings.Contains(r.out, "api@W  tools@W  web@main") {
		t.Errorf("list:\n%s", r.out)
	}
}
