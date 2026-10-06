package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldebello/wt/internal/testutil"
)

// fakeZsh puts a "zsh" on PATH and in $SHELL: `-ic alias` prints test
// aliases (and logs the lookup), anything else runs with /bin/sh.
func fakeZsh(t *testing.T, env *testutil.Env) (lookups func() int) {
	t.Helper()
	log := filepath.Join(env.Root, "alias-lookups")
	dir := testutil.StubBinary(t, "zsh", `if [ "$1" = "-ic" ] && [ "$2" = "alias" ]; then
  echo x >> `+log+`
  echo "gst='git status --short'"
  echo "gcam='git commit --all -m'"
  echo "gp='git push'"
  echo "gwip='current_branch_fn --wip'"
  exit 0
fi
shift
exec /bin/sh -c "$@"`)
	t.Setenv("SHELL", filepath.Join(dir, "zsh"))
	return func() int {
		data, _ := os.ReadFile(log)
		return strings.Count(string(data), "x")
	}
}

func TestWorkspaceRun(t *testing.T) {
	env := setupRepos(t, "api", "web")
	lookups := fakeZsh(t, env)
	mustRun(t, env, nil, "ws", "W", "--repos", "api,web")
	t.Chdir(wsPath(env, "W", "api")) // the workspace is found from here

	r := mustRun(t, env, nil, "ws", "run", `echo "$WT_REPO on $WT_BRANCH in $WT_WORKSPACE"`)
	for _, want := range []string{"── api ── ok\napi on W in W", "── web ── ok\nweb on W in W", "api ok · web ok"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("output missing %q:\n%s", want, r.out)
		}
	}
	if lookups() != 0 {
		t.Error("aliases looked up for a command without aliases")
	}

	// Aliases are expanded with a single lookup for all repositories.
	testutil.WriteFile(t, wsPath(env, "W", "web", "new.txt"), "x")
	r = mustRun(t, env, nil, "ws", "run", "gst")
	if !strings.Contains(r.out, "── web ── ok\n?? new.txt") || lookups() != 1 {
		t.Errorf("lookups = %d, output:\n%s", lookups(), r.out)
	}

	// Several words are quoted back; -- keeps flags for the command.
	r = mustRun(t, env, nil, "ws", "run", "--repos", "api", "--", "echo", "a  b", "-n")
	if !strings.Contains(r.out, "── api ── ok\na  b -n") || strings.Contains(r.out, "web") {
		t.Errorf("output:\n%s", r.out)
	}

	// A failure in one repository fails the command but runs the others.
	r = run(t, env, nil, "ws", "run", `test "$WT_REPO" = api`)
	if r.e == nil || !strings.Contains(r.e.Error(), "1 of 2 repositories failed") || !strings.Contains(r.out, "web failed (exit 1)") {
		t.Errorf("err = %v, output:\n%s", r.e, r.out)
	}

	// Outside a workspace, -w is needed.
	t.Chdir(env.Root)
	if r = run(t, env, nil, "ws", "run", "true"); r.e == nil || !strings.Contains(r.e.Error(), "-w <workspace>") {
		t.Errorf("expected -w hint, got %v", r.e)
	}
	mustRun(t, env, nil, "ws", "run", "-w", "W", "true")
	if r = run(t, env, nil, "ws", "run", "-w", "W", "--repos", "nope", "true"); r.e == nil {
		t.Error("expected unknown repo error")
	}
}

func TestSavedCommands(t *testing.T) {
	env := setupRepos(t, "api", "web")
	fakeZsh(t, env)
	mustRun(t, env, nil, "ws", "W", "--repos", "api,web")

	// Saving expands aliases and shows what will run.
	r := mustRun(t, env, nil, "commands", "commit", `gcam "$1" && gp`, "--args", "message")
	if !strings.Contains(r.out, "Saved command commit <message> (parallel):\n  git commit --all -m \"$1\" && git push") ||
		!strings.Contains(r.out, "Aliases were expanded") {
		t.Errorf("output:\n%s", r.out)
	}
	mustRun(t, env, nil, "commands", "status", "git status --short", "--serial")

	r = mustRun(t, env, nil, "commands")
	if !strings.Contains(squash(r.out), `commit <message> parallel git commit --all -m "$1" && git push`) ||
		!strings.Contains(squash(r.out), "status serial git status --short") {
		t.Errorf("list:\n%s", r.out)
	}
	if r = mustRun(t, env, nil, "commands", "commit"); !strings.Contains(r.out, "commit <message> (parallel)") {
		t.Errorf("show:\n%s", r.out)
	}

	// Arguments are checked before anything runs.
	r = run(t, env, nil, "ws", "run", "-w", "W", "-c", "commit")
	if r.e == nil || !strings.Contains(r.e.Error(), "commit needs 1 argument(s): <message>") || strings.Contains(r.out, "──") {
		t.Errorf("err = %v, output:\n%s", r.e, r.out)
	}
	if r = run(t, env, nil, "ws", "run", "-w", "W", "-c", "commit", "a", "b"); r.e == nil || !strings.Contains(r.e.Error(), "takes 1 argument(s), got 2") {
		t.Errorf("err = %v", r.e)
	}

	// The real flow: commit and push in every repository at once.
	for _, repo := range []string{"api", "web"} {
		testutil.WriteFile(t, wsPath(env, "W", repo, "README.md"), "changed "+repo+"\n")
	}
	r = mustRun(t, env, nil, "ws", "run", "-w", "W", "-c", "commit", "fix login")
	if !strings.Contains(r.out, "api ok · web ok") {
		t.Fatalf("output:\n%s", r.out)
	}
	for _, repo := range []string{"api", "web"} {
		got := testutil.Git(t, filepath.Join(env.Remote, repo), "log", "-1", "--format=%s", "W")
		if got != "fix login" {
			t.Errorf("%s: origin W head = %q", repo, got)
		}
	}

	// Saving refuses shell functions, mismatched --args and reserved names.
	if r = run(t, env, nil, "commands", "wip", "gwip"); r.e == nil || !strings.Contains(r.e.Error(), "current_branch_fn: not a program") {
		t.Errorf("err = %v", r.e)
	}
	if r = run(t, env, nil, "commands", "x", `echo "$1 $2"`, "--args", "one"); r.e == nil || !strings.Contains(r.e.Error(), "uses 2 argument(s)") {
		t.Errorf("err = %v", r.e)
	}
	if r = run(t, env, nil, "commands", "list", "echo"); r.e == nil {
		t.Error("expected reserved name error")
	}
	if r = run(t, env, nil, "ws", "run", "-w", "W", "-c", "nope"); r.e == nil || !strings.Contains(r.e.Error(), "unknown command") {
		t.Errorf("err = %v", r.e)
	}

	mustRun(t, env, nil, "commands", "remove", "status")
	if r = mustRun(t, env, nil, "commands", "list"); strings.Contains(r.out, "status") {
		t.Errorf("status not removed:\n%s", r.out)
	}
	if got := completions(t, env, "ws", "run", "-c", ""); len(got) != 1 || got[0] != "commit" {
		t.Errorf("completion = %v", got)
	}
}
