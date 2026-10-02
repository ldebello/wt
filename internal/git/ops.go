package git

import (
	"bufio"
	"context"
	"errors"
	"strconv"
	"strings"
)

// Worktree is one entry of `git worktree list --porcelain`.
type Worktree struct {
	Path     string
	Head     string
	Branch   string // short name; empty when detached
	Bare     bool
	Detached bool
	Prunable bool
}

// Worktrees lists the worktrees of the repository at dir.
func Worktrees(ctx context.Context, dir string) ([]Worktree, error) {
	out, err := Run(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(out), nil
}

func parseWorktrees(out string) []Worktree {
	var list []Worktree
	var cur *Worktree
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		key, value, _ := strings.Cut(line, " ")
		switch key {
		case "worktree":
			list = append(list, Worktree{Path: value})
			cur = &list[len(list)-1]
		case "HEAD":
			cur.Head = value
		case "branch":
			cur.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "bare":
			cur.Bare = true
		case "detached":
			cur.Detached = true
		case "prunable":
			cur.Prunable = true
		}
	}
	return list
}

// WorktreeForBranch returns the path of the worktree that has branch checked
// out, or "" if none does.
func WorktreeForBranch(ctx context.Context, dir, branch string) (string, error) {
	list, err := Worktrees(ctx, dir)
	if err != nil {
		return "", err
	}
	for _, wt := range list {
		if wt.Branch == branch && !wt.Prunable {
			return wt.Path, nil
		}
	}
	return "", nil
}

// RefExists reports whether a fully-qualified ref exists.
func RefExists(ctx context.Context, dir, ref string) bool {
	return OK(ctx, dir, "show-ref", "--verify", "--quiet", ref)
}

// LocalBranchExists reports whether refs/heads/<branch> exists.
func LocalBranchExists(ctx context.Context, dir, branch string) bool {
	return RefExists(ctx, dir, "refs/heads/"+branch)
}

// RemoteBranchExists reports whether refs/remotes/origin/<branch> exists.
func RemoteBranchExists(ctx context.Context, dir, branch string) bool {
	return RefExists(ctx, dir, "refs/remotes/origin/"+branch)
}

// DefaultBranch returns the remote default branch (e.g. "main") from
// origin/HEAD, falling back to the first of main/master/develop that exists.
func DefaultBranch(ctx context.Context, dir string) (string, error) {
	if ref, err := Run(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimPrefix(ref, "origin/"), nil
	}
	for _, b := range []string{"main", "master", "develop"} {
		if RemoteBranchExists(ctx, dir, b) {
			return b, nil
		}
	}
	return "", errors.New("cannot determine the default branch: origin/HEAD is not set (run 'wt sync' or 'wt doctor --fix')")
}

// ValidBranchName reports whether name is a valid branch name.
func ValidBranchName(ctx context.Context, name string) bool {
	return OK(ctx, "", "check-ref-format", "--branch", name)
}

// HasUpstream reports whether branch has an upstream configured (set by
// `git push -u`, push.autoSetupRemote or --track).
func HasUpstream(ctx context.Context, dir, branch string) bool {
	return OK(ctx, dir, "config", "--get", "branch."+branch+".merge")
}

// IsDirty reports whether the working tree at dir has uncommitted or
// untracked changes.
func IsDirty(ctx context.Context, dir string) (bool, error) {
	out, err := Run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// CountCommits returns the number of commits reachable from to but not from
// from (git rev-list --count from..to).
func CountCommits(ctx context.Context, dir, from, to string) (int, error) {
	out, err := Run(ctx, dir, "rev-list", "--count", from+".."+to)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(out)
}

// IsAncestor reports whether ancestor is reachable from descendant.
func IsAncestor(ctx context.Context, dir, ancestor, descendant string) bool {
	return OK(ctx, dir, "merge-base", "--is-ancestor", ancestor, descendant)
}

// Merged reports whether merging ref into base would change nothing, i.e. its
// changes are already in base. This covers regular, rebase and squash merges.
// Requires git >= 2.38 (merge-tree --write-tree).
func Merged(ctx context.Context, dir, base, ref string) (bool, error) {
	if IsAncestor(ctx, dir, ref, base) {
		return true, nil
	}
	baseTree, err := Run(ctx, dir, "rev-parse", base+"^{tree}")
	if err != nil {
		return false, err
	}
	// A conflicting merge exits non-zero: treat it as "not merged".
	out, err := Run(ctx, dir, "merge-tree", "--write-tree", base, ref)
	if err != nil {
		return false, nil
	}
	tree, _, _ := strings.Cut(out, "\n")
	return tree == baseTree, nil
}
