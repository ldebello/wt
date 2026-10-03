package repo

import (
	"context"
	"os"

	"github.com/ldebello/wt/internal/git"
)

// Sync fetches name, refreshes origin/HEAD, prunes stale worktrees and moves
// the primary checkout to the latest origin/<default>. The primary checkout is
// only moved when that loses nothing: it must be clean, detached, and contain
// no commits missing from origin/<default>. It returns a short description of
// what happened to the primary checkout.
func (ix Index) Sync(ctx context.Context, name string) (string, error) {
	bare := ix.BarePath(name)
	if _, err := git.Run(ctx, bare, "fetch", "-q", "--prune", "origin"); err != nil {
		return "", err
	}
	// Picks up a renamed default branch; failure is not fatal.
	_, _ = git.Run(ctx, bare, "remote", "set-head", "origin", "--auto")
	if _, err := git.Run(ctx, bare, "worktree", "prune"); err != nil {
		return "", err
	}
	def, err := git.DefaultBranch(ctx, bare)
	if err != nil {
		return "", err
	}
	return ix.updatePrimary(ctx, name, def)
}

// RefreshPrimary brings the primary checkout of name up to date cheaply: it
// asks origin only about the default branch (fetching it if it changed) and
// then moves the primary checkout under the same safety rules as Sync.
func (ix Index) RefreshPrimary(ctx context.Context, name string) (string, error) {
	def, err := git.DefaultBranch(ctx, ix.BarePath(name))
	if err != nil {
		return "", err
	}
	if err := ix.FetchBranches(ctx, name, []string{def}); err != nil {
		return "", err
	}
	return ix.updatePrimary(ctx, name, def)
}

func (ix Index) updatePrimary(ctx context.Context, name, def string) (string, error) {
	primary := ix.PrimaryPath(name)
	if _, err := os.Stat(primary); err != nil {
		return "primary checkout missing (run 'wt doctor --fix')", nil
	}
	target := "origin/" + def
	if dirty, err := git.IsDirty(ctx, primary); err != nil {
		return "", err
	} else if dirty {
		return "primary has local changes, not updated", nil
	}
	if branch, err := git.Run(ctx, primary, "symbolic-ref", "-q", "--short", "HEAD"); err == nil {
		return "primary is on branch " + branch + ", not updated", nil
	}
	head, err := git.Run(ctx, primary, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	want, err := git.Run(ctx, primary, "rev-parse", target)
	if err != nil {
		return "", err
	}
	if head == want {
		return "primary up to date (" + target + ")", nil
	}
	if !git.IsAncestor(ctx, primary, head, target) {
		return "primary has commits not in " + target + ", not updated", nil
	}
	if _, err := git.Run(ctx, primary, "checkout", "-q", "--detach", target); err != nil {
		return "", err
	}
	return "primary updated to " + target + " (" + want[:7] + ")", nil
}
