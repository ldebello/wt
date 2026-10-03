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

	r := mustRun(t, env, nil, "ws", "PROJ-1", "--repos", "api,web@main")
	for _, want := range []string{"Workspace PROJ-1", "api", "new branch PROJ-1 from origin/main", "web", "branch main tracking origin/main"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("output missing %q:\n%s", want, r.out)
		}
	}

	// Without --repos/--bundles the picker opens, with members preselected.
	// Members stay untouched even if deselected.
	fake := &fakeUI{multi: [][]string{{"repo:tools"}}}
	r = mustRun(t, env, fake, "workspace", "PROJ-1")
	if !strings.Contains(r.out, "tools") || strings.Contains(r.out, "api") {
		t.Errorf("unexpected output:\n%s", r.out)
	}
	// Sorted: api, tools, web.
	if opts := fake.offered[0]; len(opts) != 3 || !opts[0].Selected || opts[1].Selected || !opts[2].Selected || opts[0].Value != "repo:api" {
		t.Errorf("picker options: %+v", opts)
	}
	if fake.asked[0] != "Add to workspace PROJ-1" {
		t.Errorf("asked %v", fake.asked)
	}

	testutil.WriteFile(t, wsPath(env, "PROJ-1", "web", "dirty.txt"), "x")
	r = mustRun(t, env, nil, "ws", "list")
	if !strings.Contains(r.out, "PROJ-1") || !strings.Contains(r.out, "api@PROJ-1  tools@PROJ-1  web@main*") {
		t.Errorf("list output:\n%s", r.out)
	}

	// Partial removal.
	r = mustRun(t, env, nil, "ws", "remove", "PROJ-1", "--repos", "tools", "--yes")
	if !strings.Contains(r.out, "tools") || !strings.Contains(r.out, "worktree removed, branch PROJ-1 deleted") {
		t.Errorf("remove output:\n%s", r.out)
	}
	if _, err := os.Stat(wsPath(env, "PROJ-1", "tools")); !os.IsNotExist(err) {
		t.Error("tools not removed")
	}

	// Full removal keeps the dirty worktree and the directory.
	r = mustRun(t, env, nil, "ws", "remove", "PROJ-1", "--yes")
	if !strings.Contains(r.out, "kept: has uncommitted changes") || !strings.Contains(r.out, "some repositories could not be removed") {
		t.Errorf("remove output:\n%s", r.out)
	}
	os.Remove(wsPath(env, "PROJ-1", "web", "dirty.txt"))
	r = mustRun(t, env, nil, "ws", "remove", "PROJ-1", "--yes")
	if !strings.Contains(r.out, "Removed workspace PROJ-1") {
		t.Errorf("remove output:\n%s", r.out)
	}
	if _, err := os.Stat(wsPath(env, "PROJ-1")); !os.IsNotExist(err) {
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

func TestWorkspaceChecksOriginForNewerBranches(t *testing.T) {
	env := testutil.Setup(t)
	up := env.NewUpstream(t, "api", "main")
	mustRun(t, env, nil, "clone", up)

	// A teammate pushes PROJ-1 after our clone/sync.
	testutil.Commit(t, up, "PROJ-1", "theirs.txt", "x")

	r := mustRun(t, env, nil, "ws", "Offline", "--repos", "api@PROJ-1", "--no-fetch")
	if !strings.Contains(r.out, "new branch PROJ-1 from origin/main") {
		t.Errorf("--no-fetch should use stale refs:\n%s", r.out)
	}
	mustRun(t, env, nil, "ws", "remove", "Offline", "--yes")

	r = mustRun(t, env, nil, "ws", "PROJ-1", "--repos", "api")
	if !strings.Contains(r.out, "branch PROJ-1 tracking origin/PROJ-1") {
		t.Errorf("expected the pushed branch to be found:\n%s", r.out)
	}
	if _, err := os.Stat(wsPath(env, "PROJ-1", "api", "theirs.txt")); err != nil {
		t.Error("worktree does not have the teammate's commit")
	}
}

func TestWorkspaceWorksWhenOriginIsUnreachable(t *testing.T) {
	env := testutil.Setup(t)
	up := env.NewUpstream(t, "api", "main")
	mustRun(t, env, nil, "clone", up)
	os.RemoveAll(up)

	r := mustRun(t, env, nil, "ws", "W", "--repos", "api")
	if !strings.Contains(r.err, "could not check origin for api, using local refs") || !strings.Contains(r.out, "new branch W from origin/main") {
		t.Errorf("stdout:\n%s\nstderr:\n%s", r.out, r.err)
	}
}

func TestWorkspacePickerBranchChoices(t *testing.T) {
	env := testutil.Setup(t)
	up := env.NewUpstream(t, "api", "main")
	testutil.Commit(t, up, "feature/x", "x.txt", "x")
	testutil.Commit(t, up, "W", "w.txt", "w") // same name as the workspace
	mustRun(t, env, nil, "clone", up)
	mustRun(t, env, nil, "clone", env.NewUpstream(t, "web", "main"))
	mustRun(t, env, nil, "ws", "W", "--repos", "web")

	fake := &fakeUI{multi: [][]string{{"repo:api=@feature/x"}}}
	r := mustRun(t, env, fake, "ws", "W")
	if !strings.Contains(r.out, "branch feature/x tracking origin/feature/x") {
		t.Errorf("output:\n%s", r.out)
	}

	opts := fake.offered[0] // api, web
	if opts[1].Choices != nil || !strings.Contains(opts[1].Label, "in workspace on W") {
		t.Errorf("existing member should not offer branches: %+v", opts[1])
	}
	choices, err := opts[0].Choices()
	if err != nil {
		t.Fatal(err)
	}
	var values []string
	for _, c := range choices {
		values = append(values, c.Value)
	}
	// Default behaviour first, then the default branch, then the rest; the
	// workspace branch is not repeated.
	if len(values) != 3 || values[0] != "" || values[1] != "?main" || values[2] != "?feature/x" {
		t.Errorf("choices = %+v", choices)
	}
	if !strings.Contains(choices[0].Label, "W  (workspace branch") || !strings.Contains(choices[1].Label, "(default branch)") {
		t.Errorf("labels = %+v", choices)
	}
	// Each branch then asks: create the workspace branch from it, or work on it.
	next := choices[2].Next
	if len(next) != 2 || next[0].Value != ":feature/x" || next[1].Value != "@feature/x" ||
		!strings.Contains(next[0].Label, "Create W from feature/x") || !strings.Contains(next[1].Label, "Work directly on feature/x") {
		t.Errorf("next = %+v", next)
	}
}

func TestWorkspaceBaseVersusBranch(t *testing.T) {
	env := testutil.Setup(t)
	billing := env.NewUpstream(t, "billing", "main")
	web := env.NewUpstream(t, "web", "main")
	testutil.Commit(t, billing, "release-2.4", "fix.txt", "release")
	testutil.Commit(t, web, "develop", "dev.txt", "develop")
	mustRun(t, env, nil, "clone", billing)
	mustRun(t, env, nil, "clone", web)

	// repo:base -> own branch from the base, per repository.
	r := mustRun(t, env, nil, "ws", "HOTFIX-77", "--repos", "billing:release-2.4,web:develop")
	for _, want := range []string{"new branch HOTFIX-77 from origin/release-2.4", "new branch HOTFIX-77 from origin/develop"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("output missing %q:\n%s", want, r.out)
		}
	}
	// A second hotfix from the same base works: each has its own branch.
	mustRun(t, env, nil, "ws", "HOTFIX-78", "--repos", "billing:release-2.4")
	r = mustRun(t, env, nil, "ws", "list")
	if !strings.Contains(r.out, "HOTFIX-77  billing@HOTFIX-77  web@HOTFIX-77") || !strings.Contains(r.out, "HOTFIX-78  billing@HOTFIX-78") {
		t.Errorf("list:\n%s", r.out)
	}
	if _, err := os.Stat(wsPath(env, "HOTFIX-78", "billing", "fix.txt")); err != nil {
		t.Error("HOTFIX-78 not based on release-2.4")
	}

	// repo@branch -> the branch itself, so only one workspace can have it.
	r = mustRun(t, env, nil, "ws", "REL-A", "--repos", "billing@release-2.4")
	if !strings.Contains(r.out, "branch release-2.4 tracking origin/release-2.4") {
		t.Errorf("output:\n%s", r.out)
	}
	r = run(t, env, nil, "ws", "REL-B", "--repos", "billing@release-2.4")
	if r.e == nil || !strings.Contains(r.e.Error(), "already checked out at") {
		t.Errorf("expected branch-in-use error, got %v", r.e)
	}

	if r := run(t, env, nil, "ws", "X", "--repos", "billing:nope"); r.e == nil || !strings.Contains(r.e.Error(), `base branch "nope"`) {
		t.Errorf("expected missing base error, got %v", r.e)
	}
	if r := run(t, env, nil, "ws", "X", "--from", "main"); r.e == nil {
		t.Error("--from should no longer exist")
	}
}
