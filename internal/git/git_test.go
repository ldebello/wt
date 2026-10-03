package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldebello/wt/internal/testutil"
)

func TestParseWorktrees(t *testing.T) {
	out := `worktree /repos/x.git
bare

worktree /repos/x
HEAD 1111
detached

worktree /ws/A/x
HEAD 2222
branch refs/heads/feature/a

worktree /ws/B/x
HEAD 3333
branch refs/heads/B
prunable gitdir file points to non-existent location
`
	got := parseWorktrees(out)
	if len(got) != 4 {
		t.Fatalf("got %d worktrees: %+v", len(got), got)
	}
	if !got[0].Bare || !got[1].Detached || got[1].Head != "1111" {
		t.Errorf("unexpected: %+v", got[:2])
	}
	if got[2].Branch != "feature/a" || got[2].Path != "/ws/A/x" {
		t.Errorf("unexpected: %+v", got[2])
	}
	if !got[3].Prunable {
		t.Errorf("expected prunable: %+v", got[3])
	}
}

func TestRunIncludesStderr(t *testing.T) {
	testutil.Setup(t)
	_, err := Run(context.Background(), t.TempDir(), "rev-parse", "HEAD")
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("expected stderr in error, got %v", err)
	}
}

func TestDefaultBranchAndRefs(t *testing.T) {
	env := testutil.Setup(t)
	ctx := context.Background()
	up := env.NewUpstream(t, "app", "trunk")
	testutil.Commit(t, up, "feature", "f.txt", "x")

	clone := filepath.Join(env.Root, "clone")
	testutil.Git(t, "", "clone", "-q", up, clone)

	got, err := DefaultBranch(ctx, clone)
	if err != nil || got != "trunk" {
		t.Fatalf("DefaultBranch = %q, %v", got, err)
	}
	if !RemoteBranchExists(ctx, clone, "feature") || RemoteBranchExists(ctx, clone, "nope") {
		t.Error("RemoteBranchExists wrong")
	}
	if !LocalBranchExists(ctx, clone, "trunk") || LocalBranchExists(ctx, clone, "feature") {
		t.Error("LocalBranchExists wrong")
	}

	// Fallback when origin/HEAD is missing.
	testutil.Git(t, clone, "remote", "set-head", "origin", "--delete")
	// git >= 2.48 recreates origin/HEAD on fetch unless told not to.
	testutil.Git(t, clone, "config", "remote.origin.followRemoteHEAD", "never")
	if _, err := DefaultBranch(ctx, clone); err == nil {
		t.Error("expected error without origin/HEAD and no main/master/develop")
	}
	testutil.Git(t, up, "branch", "main")
	testutil.Git(t, clone, "fetch", "-q")
	if got, err := DefaultBranch(ctx, clone); err != nil || got != "main" {
		t.Errorf("fallback DefaultBranch = %q, %v", got, err)
	}
}

func TestMergedDetectsRegularAndSquashMerges(t *testing.T) {
	env := testutil.Setup(t)
	ctx := context.Background()
	repo := env.NewUpstream(t, "app", "main")

	testutil.Commit(t, repo, "regular", "a.txt", "a")
	testutil.Commit(t, repo, "squashed", "b.txt", "b1")
	testutil.Commit(t, repo, "squashed", "b.txt", "b2")
	testutil.Commit(t, repo, "open", "c.txt", "c")

	testutil.Git(t, repo, "merge", "-q", "--no-ff", "-m", "merge regular", "regular")
	testutil.Git(t, repo, "merge", "-q", "--squash", "squashed")
	testutil.Git(t, repo, "commit", "-q", "-m", "squash")
	testutil.Commit(t, repo, "main", "later.txt", "main moved on")

	for branch, want := range map[string]bool{"regular": true, "squashed": true, "open": false} {
		got, err := Merged(ctx, repo, "main", branch)
		if err != nil || got != want {
			t.Errorf("Merged(%s) = %v, %v; want %v", branch, got, err, want)
		}
	}
	if n, _ := CountCommits(ctx, repo, "main", "open"); n != 1 {
		t.Errorf("CountCommits = %d", n)
	}
}

func TestWorktreeForBranchAndDirty(t *testing.T) {
	env := testutil.Setup(t)
	ctx := context.Background()
	repo := env.NewUpstream(t, "app", "main")
	wt := filepath.Join(env.Root, "wt-feature")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "feature", wt)

	got, err := WorktreeForBranch(ctx, repo, "feature")
	if err != nil || got != wt {
		t.Fatalf("WorktreeForBranch = %q, %v", got, err)
	}
	if got, _ := WorktreeForBranch(ctx, repo, "other"); got != "" {
		t.Errorf("expected no worktree, got %q", got)
	}

	if dirty, _ := IsDirty(ctx, wt); dirty {
		t.Error("fresh worktree reported dirty")
	}
	if err := os.WriteFile(filepath.Join(wt, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dirty, _ := IsDirty(ctx, wt); !dirty {
		t.Error("untracked file not reported dirty")
	}
}

func TestBranches(t *testing.T) {
	env := testutil.Setup(t)
	up := env.NewUpstream(t, "app", "main")
	testutil.Commit(t, up, "older", "o.txt", "o")
	testutil.Git(t, up, "commit", "-q", "--allow-empty", "-m", "newer", "--date", "2030-01-01T00:00:00")
	clone := filepath.Join(env.Root, "clone")
	testutil.Git(t, "", "clone", "-q", up, clone)
	testutil.Git(t, clone, "branch", "local-only", "origin/older")

	got, err := Branches(context.Background(), clone)
	if err != nil {
		t.Fatal(err)
	}
	// main (local + origin) once, no origin/HEAD.
	joined := strings.Join(got, ",")
	if strings.Count(joined, "main") != 1 || strings.Contains(joined, "HEAD") || !strings.Contains(joined, "local-only") || !strings.Contains(joined, "older") {
		t.Errorf("Branches = %v", got)
	}
}
