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

func TestSyncUpdatesPrimarySafely(t *testing.T) {
	env := testutil.Setup(t)
	ctx := context.Background()
	up := env.NewUpstream(t, "app", "main")
	ix := Index{Dir: filepath.Join(env.Home, ".repos")}
	c, err := ix.Clone(ctx, up, "", io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	sync := func() string {
		t.Helper()
		msg, err := ix.Sync(ctx, "app")
		if err != nil {
			t.Fatal(err)
		}
		return msg
	}

	if msg := sync(); !strings.Contains(msg, "up to date") {
		t.Errorf("msg = %q", msg)
	}

	testutil.Commit(t, up, "main", "new.txt", "1")
	if msg := sync(); !strings.Contains(msg, "updated to origin/main") {
		t.Errorf("msg = %q", msg)
	}
	if _, err := os.Stat(filepath.Join(c.Primary, "new.txt")); err != nil {
		t.Error("primary not updated")
	}

	// Local changes block the update.
	testutil.Commit(t, up, "main", "new.txt", "2")
	testutil.WriteFile(t, filepath.Join(c.Primary, "scratch.txt"), "x")
	if msg := sync(); !strings.Contains(msg, "local changes") {
		t.Errorf("msg = %q", msg)
	}
	os.Remove(filepath.Join(c.Primary, "scratch.txt"))

	// Local commits on the detached HEAD block the update.
	testutil.WriteFile(t, filepath.Join(c.Primary, "mine.txt"), "x")
	testutil.Git(t, c.Primary, "add", "mine.txt")
	testutil.Git(t, c.Primary, "commit", "-q", "-m", "local")
	if msg := sync(); !strings.Contains(msg, "commits not in origin/main") {
		t.Errorf("msg = %q", msg)
	}

	// An attached branch is left alone.
	testutil.Git(t, c.Primary, "switch", "-q", "-c", "local-work")
	if msg := sync(); !strings.Contains(msg, "on branch local-work") {
		t.Errorf("msg = %q", msg)
	}

	os.RemoveAll(c.Primary)
	if msg := sync(); !strings.Contains(msg, "missing") {
		t.Errorf("msg = %q", msg)
	}
}

func TestFetchBranches(t *testing.T) {
	env := testutil.Setup(t)
	ctx := context.Background()
	up := env.NewUpstream(t, "app", "main")
	ix := Index{Dir: filepath.Join(env.Home, ".repos")}
	c, err := ix.Clone(ctx, up, "", io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	// Pushed after the clone: the local refs don't know about them yet.
	mainSHA := testutil.Commit(t, up, "main", "m.txt", "m")
	xSHA := testutil.Commit(t, up, "X", "x.txt", "x")
	testutil.Commit(t, up, "feature/X", "f.txt", "f") // matches "X" as a suffix

	if err := ix.FetchBranches(ctx, "app", []string{"X", "main", "missing"}); err != nil {
		t.Fatal(err)
	}
	if got := testutil.Git(t, c.Bare, "rev-parse", "origin/X"); got != xSHA {
		t.Errorf("origin/X = %s; want %s", got, xSHA)
	}
	if got := testutil.Git(t, c.Bare, "rev-parse", "origin/main"); got != mainSHA {
		t.Errorf("origin/main = %s; want %s", got, mainSHA)
	}
	if testutil.Git(t, c.Bare, "for-each-ref", "refs/remotes/origin/feature") != "" {
		t.Error("fetched a branch that was not asked for")
	}
	// Nothing changed: no error, nothing to fetch.
	if err := ix.FetchBranches(ctx, "app", []string{"X"}); err != nil {
		t.Fatal(err)
	}
}
