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

func TestNameFromURL(t *testing.T) {
	for url, want := range map[string]string{
		"git@github.com:acme/billing.git":     "billing",
		"https://github.com/acme/billing.git": "billing",
		"https://github.com/acme/billing":     "billing",
		"https://github.com/acme/billing/":    "billing",
		"/tmp/remote/app":                     "app",
		"git@host:app.git":                    "app",
	} {
		got, err := NameFromURL(url)
		if err != nil || got != want {
			t.Errorf("NameFromURL(%q) = %q, %v; want %q", url, got, err, want)
		}
	}
	if _, err := NameFromURL("/"); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"billing", "PROJ-12", "a.b_c"} {
		if err := ValidName("x", ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-x", "a/b", ".hidden", "x.git", "a b"} {
		if err := ValidName("x", bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestClone(t *testing.T) {
	env := testutil.Setup(t)
	ctx := context.Background()
	up := env.NewUpstream(t, "app", "trunk")
	testutil.Commit(t, up, "feature", "f.txt", "x")
	ix := Index{Dir: filepath.Join(env.Home, ".repos")}

	c, err := ix.Clone(ctx, up, "", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "app" || c.DefaultBranch != "trunk" {
		t.Errorf("unexpected clone: %+v", c)
	}
	if got := testutil.Git(t, c.Bare, "rev-parse", "--is-bare-repository"); got != "true" {
		t.Errorf("bare = %s", got)
	}
	// Remote branches are remote-tracking refs, not local branches.
	if refs := testutil.Git(t, c.Bare, "for-each-ref", "--format=%(refname)", "refs/heads"); refs != "" {
		t.Errorf("unexpected local branches: %s", refs)
	}
	if testutil.Git(t, c.Bare, "rev-parse", "--verify", "refs/remotes/origin/feature") == "" {
		t.Error("origin/feature missing")
	}
	if got := testutil.Git(t, c.Bare, "config", "push.autoSetupRemote"); got != "true" {
		t.Errorf("push.autoSetupRemote = %q", got)
	}
	// Primary checkout is detached at origin/trunk.
	if _, err := os.Stat(filepath.Join(c.Primary, "README.md")); err != nil {
		t.Errorf("primary checkout missing files: %v", err)
	}
	if out := testutil.Git(t, c.Primary, "status", "--porcelain=v2", "--branch"); !strings.Contains(out, "branch.head (detached)") {
		t.Errorf("primary not detached:\n%s", out)
	}
	if names, _ := ix.List(); len(names) != 1 || names[0] != "app" {
		t.Errorf("List = %v", names)
	}
	if !ix.Exists("app") || ix.Require("nope") == nil {
		t.Error("Exists/Require wrong")
	}
	if got := ix.NameForGitDir(c.Bare); got != "app" {
		t.Errorf("NameForGitDir = %q", got)
	}
	// git records resolved paths: an index reached through a symlink still
	// recognizes its repositories.
	link := filepath.Join(env.Root, "repos-link")
	if err := os.Symlink(ix.Dir, link); err != nil {
		t.Fatal(err)
	}
	if got := (Index{Dir: link}).NameForGitDir(c.Bare); got != "app" {
		t.Errorf("NameForGitDir through symlink = %q", got)
	}
	if got := ix.NameForGitDir(filepath.Join(env.Root, "other", "app.git")); got != "" {
		t.Errorf("NameForGitDir outside the index = %q", got)
	}

	// Name collision is an actionable error; --name works around it.
	if _, err := ix.Clone(ctx, up, "", io.Discard); err == nil || !strings.Contains(err.Error(), "--name") {
		t.Errorf("expected collision error, got %v", err)
	}
	if _, err := ix.Clone(ctx, up, "app2", io.Discard); err != nil {
		t.Errorf("clone with alias: %v", err)
	}
}

func TestCloneFailureCleansUp(t *testing.T) {
	env := testutil.Setup(t)
	ix := Index{Dir: filepath.Join(env.Home, ".repos")}
	if _, err := ix.Clone(context.Background(), filepath.Join(env.Remote, "missing"), "", io.Discard); err == nil {
		t.Fatal("expected error")
	}
	if ix.Exists("missing") {
		t.Error("bare repo left behind")
	}
	if _, err := os.Stat(ix.PrimaryPath("missing")); err == nil {
		t.Error("primary left behind")
	}
}
