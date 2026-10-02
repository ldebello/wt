// Package doctor runs health checks and optional fixes.
package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ldebello/wt/internal/git"
	"github.com/ldebello/wt/internal/integration"
	"github.com/ldebello/wt/internal/repo"
	"github.com/ldebello/wt/internal/workspace"
)

// Level is a finding's severity.
type Level int

const (
	OK Level = iota
	Warn
	Error
)

func (l Level) String() string {
	return [...]string{"ok", "warn", "error"}[l]
}

// Finding is the result of one check.
type Finding struct {
	Area    string
	Level   Level
	Message string
	Hint    string                      // how to fix it by hand
	fix     func(context.Context) error // automatic fix, if any
}

// Fixable reports whether --fix can repair the finding.
func (f Finding) Fixable() bool { return f.fix != nil }

// Fix applies the automatic fix.
func (f Finding) Fix(ctx context.Context) error { return f.fix(ctx) }

func ok(area, msg string) Finding { return Finding{Area: area, Level: OK, Message: msg} }

// MinGit is the oldest supported git version (merge-tree --write-tree).
var MinGit = [2]int{2, 38}

// Inputs are what the checks inspect.
type Inputs struct {
	Index        repo.Index
	Workspaces   workspace.Manager
	Integrations []integration.Integration
	// Editor checks the editor configuration; nil skips the check.
	Editor func() error
	// ShellInit is the value of $WT_SHELL_INIT.
	ShellInit string
}

// Run performs every check.
func Run(ctx context.Context, in Inputs) []Finding {
	findings := []Finding{checkGit(ctx)}
	findings = append(findings, checkDir("repos", in.Index.Dir, "add one with: wt clone <url>"))
	findings = append(findings, checkDir("workspaces", in.Workspaces.Dir, "created by: wt ws <name>"))

	names, err := in.Index.List()
	if err != nil {
		findings = append(findings, Finding{Area: "repos", Level: Error, Message: err.Error()})
	}
	for _, name := range names {
		findings = append(findings, checkRepo(ctx, in.Index, name)...)
	}
	findings = append(findings, checkWorkspaces(in.Workspaces)...)

	if in.Editor != nil {
		if err := in.Editor(); err != nil {
			findings = append(findings, Finding{Area: "editor", Level: Warn, Message: firstLine(err.Error()), Hint: "configure [editor] command in settings.toml"})
		} else {
			findings = append(findings, ok("editor", "editor found"))
		}
	}
	for _, i := range in.Integrations {
		if !i.Enabled() {
			continue
		}
		if err := i.Check(); err != nil {
			findings = append(findings, Finding{Area: i.Name(), Level: Error, Message: err.Error(), Hint: "fix it or run: wt integrations " + i.Name() + " --disable"})
		} else {
			findings = append(findings, ok(i.Name(), "enabled"))
		}
	}
	if in.ShellInit == "" {
		findings = append(findings, Finding{Area: "shell", Level: Warn, Message: "shell integration not loaded ('wt cd' will not work)",
			Hint: `add to your shell profile: eval "$(wt shell-init zsh)"  (or bash; fish: wt shell-init fish | source)`})
	} else {
		findings = append(findings, ok("shell", "integration loaded ("+in.ShellInit+")"))
	}
	return findings
}

var gitVersionRE = regexp.MustCompile(`(\d+)\.(\d+)`)

func checkGit(ctx context.Context) Finding {
	if _, err := exec.LookPath("git"); err != nil {
		return Finding{Area: "git", Level: Error, Message: "git not found in PATH"}
	}
	out, err := git.Run(ctx, "", "version")
	if err != nil {
		return Finding{Area: "git", Level: Error, Message: err.Error()}
	}
	m := gitVersionRE.FindStringSubmatch(out)
	if m == nil {
		return Finding{Area: "git", Level: Warn, Message: "cannot parse " + out}
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	if major < MinGit[0] || (major == MinGit[0] && minor < MinGit[1]) {
		return Finding{Area: "git", Level: Error, Message: fmt.Sprintf("%s is too old; wt needs git >= %d.%d", out, MinGit[0], MinGit[1])}
	}
	return ok("git", out)
}

func checkDir(area, dir, hint string) Finding {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return Finding{Area: area, Level: OK, Message: dir + " does not exist yet (" + hint + ")"}
	}
	return ok(area, dir)
}

const fetchRefspec = "+refs/heads/*:refs/remotes/origin/*"

func checkRepo(ctx context.Context, ix repo.Index, name string) []Finding {
	area := "repo " + name
	bare := ix.BarePath(name)
	if out, err := git.Run(ctx, bare, "rev-parse", "--is-bare-repository"); err != nil || out != "true" {
		return []Finding{{Area: area, Level: Error, Message: bare + " is not a bare git repository", Hint: "remove it and run wt clone again"}}
	}
	if _, err := git.Run(ctx, bare, "remote", "get-url", "origin"); err != nil {
		return []Finding{{Area: area, Level: Error, Message: "no 'origin' remote", Hint: "git -C " + bare + " remote add origin <url>"}}
	}

	var findings []Finding
	problems := 0
	add := func(f Finding) {
		findings = append(findings, f)
		problems++
	}
	if refspecs, _ := git.Run(ctx, bare, "config", "--get-all", "remote.origin.fetch"); !strings.Contains(refspecs, fetchRefspec) {
		add(Finding{Area: area, Level: Warn, Message: "origin fetch refspec is not " + fetchRefspec,
			fix: func(ctx context.Context) error {
				_, err := git.Run(ctx, bare, "config", "--add", "remote.origin.fetch", fetchRefspec)
				return err
			}})
	}
	if !git.RefExists(ctx, bare, "refs/remotes/origin/HEAD") {
		add(Finding{Area: area, Level: Warn, Message: "origin/HEAD is not set (default branch unknown)",
			fix: func(ctx context.Context) error {
				_, err := git.Run(ctx, bare, "remote", "set-head", "origin", "--auto")
				return err
			}})
	}
	if worktrees, err := git.Worktrees(ctx, bare); err == nil {
		for _, wt := range worktrees {
			if wt.Prunable {
				add(Finding{Area: area, Level: Warn, Message: "stale worktree registration: " + wt.Path,
					fix: func(ctx context.Context) error {
						_, err := git.Run(ctx, bare, "worktree", "prune")
						return err
					}})
				break
			}
		}
	}
	primary := ix.PrimaryPath(name)
	if _, err := os.Stat(filepath.Join(primary, ".git")); err != nil {
		f := Finding{Area: area, Level: Error, Message: "primary checkout missing: " + primary}
		if _, statErr := os.Stat(primary); os.IsNotExist(statErr) {
			f.fix = func(ctx context.Context) error {
				def, err := git.DefaultBranch(ctx, bare)
				if err != nil {
					return err
				}
				_, _ = git.Run(ctx, bare, "worktree", "prune")
				_, err = git.Run(ctx, bare, "worktree", "add", "-q", "--detach", primary, "origin/"+def)
				return err
			}
		} else {
			f.Hint = primary + " exists but is not a checkout; move it away and run wt doctor --fix"
		}
		add(f)
	}
	if problems == 0 {
		findings = append(findings, ok(area, "healthy"))
	}
	return findings
}

func checkWorkspaces(mgr workspace.Manager) []Finding {
	list, err := mgr.List()
	if err != nil {
		return []Finding{{Area: "workspaces", Level: Error, Message: err.Error()}}
	}
	var findings []Finding
	for _, ws := range list {
		for _, m := range ws.Members {
			if m.Broken || !mgr.Index.Exists(m.Repo) {
				findings = append(findings, Finding{Area: "workspace " + ws.Name, Level: Error,
					Message: m.Repo + ": its repository is missing from " + mgr.Index.Dir,
					Hint:    "re-clone it with 'wt clone <url>', or delete " + m.Path})
			}
		}
	}
	if len(findings) == 0 {
		findings = append(findings, ok("workspaces", fmt.Sprintf("%d workspaces", len(list))))
	}
	return findings
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
