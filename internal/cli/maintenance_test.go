package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/ldebello/wt/internal/testutil"
)

func TestSyncCommand(t *testing.T) {
	env := testutil.Setup(t)
	api := env.NewUpstream(t, "api", "main")
	web := env.NewUpstream(t, "web", "main")
	mustRun(t, env, nil, "clone", api)
	mustRun(t, env, nil, "clone", web)
	testutil.Commit(t, api, "main", "new.txt", "x")

	r := mustRun(t, env, nil, "sync")
	if !strings.Contains(r.out, "api  primary updated to origin/main") || !strings.Contains(r.out, "web  primary up to date") {
		t.Errorf("output:\n%s", r.out)
	}
	r = mustRun(t, env, nil, "sync", "web")
	if strings.Contains(r.out, "api") {
		t.Errorf("synced more than asked:\n%s", r.out)
	}
	if r := run(t, env, nil, "sync", "ghost"); r.e == nil {
		t.Error("expected unknown repo error")
	}

	os.RemoveAll(web)
	r = run(t, env, nil, "sync")
	if r.e == nil || !strings.Contains(r.out, "web  error:") {
		t.Errorf("expected failure for missing remote: %v\n%s", r.e, r.out)
	}
}

func TestCleanupCommand(t *testing.T) {
	env := testutil.Setup(t)
	api := env.NewUpstream(t, "api", "main")
	mustRun(t, env, nil, "clone", api)
	mustRun(t, env, nil, "ws", "Merged", "--repos", "api")
	mustRun(t, env, nil, "ws", "Active", "--repos", "api@feature")
	mustRun(t, env, nil, "ws", "Fresh", "--repos", "api@fresh")

	testutil.Commit(t, wsPath(env, "Merged", "api"), "Merged", "m.txt", "m")
	testutil.Commit(t, api, "main", "m.txt", "m") // squash-merged upstream
	testutil.Commit(t, wsPath(env, "Active", "api"), "feature", "a.txt", "a")

	r := mustRun(t, env, nil, "cleanup", "--dry-run")
	for _, want := range []string{
		"[x] Merged (api)", "merged (safe to remove)",
		"[ ] Active (api)", "contains unpushed commits",
		"[ ] Fresh (api)", "no commits yet",
	} {
		if !strings.Contains(r.out, want) {
			t.Errorf("dry-run missing %q:\n%s", want, r.out)
		}
	}
	for _, ws := range []string{"Merged", "Active", "Fresh"} {
		if _, err := os.Stat(wsPath(env, ws)); err != nil {
			t.Errorf("dry-run removed %s", ws)
		}
	}

	if r := run(t, env, nil, "cleanup"); r.e == nil || !strings.Contains(r.e.Error(), "--yes") {
		t.Errorf("expected --yes hint, got %v", r.e)
	}

	// Interactive: the merged workspace is preselected; the user adds Fresh.
	fake := &fakeUI{multi: [][]string{{"Merged", "Fresh"}}, confirms: []bool{true}}
	r = mustRun(t, env, fake, "cleanup", "--no-fetch")
	if opts := fake.offered[0]; len(opts) != 3 || opts[0].Selected || !opts[2].Selected || opts[1].Selected {
		t.Errorf("options: %+v", opts) // sorted: Active, Fresh, Merged
	}
	if !strings.Contains(r.out, "Removed workspace Merged") || !strings.Contains(r.out, "Removed workspace Fresh") {
		t.Errorf("output:\n%s", r.out)
	}
	if _, err := os.Stat(wsPath(env, "Active")); err != nil {
		t.Error("Active removed")
	}

	// --yes removes only merged workspaces: none left.
	r = mustRun(t, env, nil, "cleanup", "--yes", "--no-fetch")
	if !strings.Contains(r.out, "Nothing to remove") {
		t.Errorf("output:\n%s", r.out)
	}
}
