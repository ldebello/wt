// Package testutil provides isolated git environments and upstream fixtures
// for tests.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Env is an isolated environment: HOME, WT_HOME and git config all point
// into a temp dir.
type Env struct {
	Root   string // temp root (symlinks resolved)
	Home   string // $HOME
	WTHome string // $WT_HOME
	Remote string // where upstream fixtures are created
}

// Setup isolates HOME, WT_HOME and git configuration for the test.
func Setup(t *testing.T) *Env {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := &Env{
		Root:   root,
		Home:   filepath.Join(root, "home"),
		WTHome: filepath.Join(root, "home", ".wt"),
		Remote: filepath.Join(root, "remote"),
	}
	for _, dir := range []string{env.Home, env.Remote} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", env.Home)
	t.Setenv("WT_HOME", env.WTHome)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "wt test")
	t.Setenv("GIT_AUTHOR_EMAIL", "wt@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "wt test")
	t.Setenv("GIT_COMMITTER_EMAIL", "wt@example.com")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	return env
}

// NewUpstream creates a non-bare repository named name under env.Remote with
// one commit on defaultBranch, accepting pushes to its checked-out branch.
// The returned path can be used as a clone URL.
func (e *Env) NewUpstream(t *testing.T, name, defaultBranch string) string {
	t.Helper()
	dir := filepath.Join(e.Remote, name)
	Git(t, "", "init", "-q", "-b", defaultBranch, dir)
	Git(t, dir, "config", "receive.denyCurrentBranch", "updateInstead")
	Commit(t, dir, defaultBranch, "README.md", "# "+name+"\n")
	return dir
}

// Git runs git in dir and returns trimmed output, failing the test on error.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// Commit writes file with content on branch in the repository (or worktree)
// at dir and commits it. If branch is not checked out at dir, a temporary
// worktree is used; a missing branch is created from dir's HEAD.
func Commit(t *testing.T, dir, branch, file, content string) string {
	t.Helper()
	work := dir
	current, _ := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "-q", "HEAD").Output()
	if strings.TrimSpace(string(current)) != branch {
		work = filepath.Join(t.TempDir(), "commit")
		if exec.Command("git", "-C", dir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil {
			Git(t, dir, "worktree", "add", "-q", work, branch)
		} else {
			Git(t, dir, "worktree", "add", "-q", "-b", branch, work)
		}
		defer Git(t, dir, "worktree", "remove", "--force", work)
	}
	path := filepath.Join(work, file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	Git(t, work, "add", "--all")
	// Include the branch so identical changes on two branches get distinct
	// commits, as with real squash merges.
	Git(t, work, "commit", "-q", "-m", "update "+file+" on "+branch)
	return Git(t, work, "rev-parse", "HEAD")
}

// WriteFile writes content to path, creating parent directories.
func WriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// StubBinary puts an executable shell script named name on PATH that runs
// script. Returns the directory containing it.
func StubBinary(t *testing.T, name, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}
