package repo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ldebello/wt/internal/git"
)

// Remove deletes repository name from the index: its primary checkout and
// its bare repository. It refuses, explaining why, when that could lose
// work or break something:
//   - another worktree (e.g. a workspace) still uses the repository;
//   - the primary checkout has uncommitted changes or commits not on origin;
//   - a local branch has commits that are not on origin (e.g. a branch kept
//     by 'wt ws remove' because it was not merged).
func (ix Index) Remove(ctx context.Context, name string) error {
	if err := ix.Require(name); err != nil {
		return err
	}
	bare, primary := ix.BarePath(name), ix.PrimaryPath(name)

	var problems []string
	worktrees, err := git.Worktrees(ctx, bare)
	if err != nil {
		return err
	}
	// git records worktree paths with symlinks resolved.
	resolvedPrimary, err := filepath.EvalSymlinks(primary)
	if err != nil {
		resolvedPrimary = filepath.Clean(primary)
	}
	for _, wt := range worktrees {
		if wt.Bare || wt.Prunable || filepath.Clean(wt.Path) == resolvedPrimary {
			continue
		}
		problems = append(problems, "used by the worktree "+wt.Path+" (remove it first, e.g. wt ws remove <workspace> --repos "+name+")")
	}

	if _, err := os.Stat(primary); err == nil {
		if dirty, err := git.IsDirty(ctx, primary); err != nil {
			return err
		} else if dirty {
			problems = append(problems, "the primary checkout "+primary+" has uncommitted changes")
		} else if !onOrigin(ctx, primary, "HEAD") {
			problems = append(problems, "the primary checkout "+primary+" has commits that are not on origin")
		}
	}

	branches, err := git.Run(ctx, bare, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return err
	}
	var unpushed []string
	for _, b := range strings.Fields(branches) {
		if !onOrigin(ctx, bare, "refs/heads/"+b) {
			unpushed = append(unpushed, b)
		}
	}
	if len(unpushed) > 0 {
		problems = append(problems, fmt.Sprintf("local branches with commits not on origin: %s (push them, or delete them with: git -C %s branch -D <branch>)",
			strings.Join(unpushed, ", "), bare))
	}

	if len(problems) > 0 {
		return fmt.Errorf("repository %s was not removed:\n  - %s", name, strings.Join(problems, "\n  - "))
	}
	if err := os.RemoveAll(primary); err != nil {
		return err
	}
	return os.RemoveAll(bare)
}

// onOrigin reports whether rev is contained in some remote-tracking branch.
func onOrigin(ctx context.Context, dir, rev string) bool {
	out, err := git.Run(ctx, dir, "for-each-ref", "--count=1", "--contains", rev, "--format=%(refname)", "refs/remotes")
	return err == nil && out != ""
}
