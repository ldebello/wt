package repo

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldebello/wt/internal/testutil"
)

func TestRemoveRefusesToLoseWork(t *testing.T) {
	env := testutil.Setup(t)
	ctx := context.Background()
	up := env.NewUpstream(t, "app", "main")
	ix := Index{Dir: filepath.Join(env.Home, ".repos")}
	c, err := ix.Clone(ctx, up, "", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	expectRefusal := func(want string) {
		t.Helper()
		err := ix.Remove(ctx, "app")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Remove: got %v, want error containing %q", err, want)
		}
		if !ix.Exists("app") {
			t.Fatal("repository deleted despite the error")
		}
	}

	// A workspace worktree still uses it.
	ws := filepath.Join(env.Root, "workspaces", "W", "app")
	testutil.Git(t, c.Bare, "worktree", "add", "-q", "-b", "W", ws, "origin/main")
	expectRefusal("used by the worktree")

	// Worktree gone, but its branch has commits that only exist locally.
	testutil.Commit(t, ws, "W", "w.txt", "work")
	testutil.Git(t, c.Bare, "worktree", "remove", ws)
	expectRefusal("local branches with commits not on origin: W")
	testutil.Git(t, c.Bare, "branch", "-D", "W")

	// Primary checkout with uncommitted changes, then with a local commit.
	testutil.WriteFile(t, filepath.Join(c.Primary, "scratch.txt"), "x")
	expectRefusal("uncommitted changes")
	testutil.Git(t, c.Primary, "add", "scratch.txt")
	testutil.Git(t, c.Primary, "commit", "-q", "-m", "local")
	expectRefusal("commits that are not on origin")
	testutil.Git(t, c.Primary, "checkout", "-q", "--detach", "origin/main")

	// A branch that is on origin is fine; so is a deleted workspace folder.
	testutil.Git(t, c.Bare, "branch", "pushed", "origin/main")
	gone := filepath.Join(env.Root, "workspaces", "Gone", "app")
	testutil.Git(t, c.Bare, "worktree", "add", "-q", "--detach", gone, "origin/main")
	os.RemoveAll(gone)

	if err := ix.Remove(ctx, "app"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{c.Bare, c.Primary} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists", p)
		}
	}
	if err := ix.Remove(ctx, "app"); err == nil || !strings.Contains(err.Error(), "unknown repository") {
		t.Errorf("second Remove: %v", err)
	}
}
