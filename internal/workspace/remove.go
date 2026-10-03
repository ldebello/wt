package workspace

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ldebello/wt/internal/git"
)

// RemoveResult reports what happened to one member.
type RemoveResult struct {
	Repo          string
	Branch        string
	Removed       bool   // worktree removed
	Skipped       string // why the worktree was kept
	BranchDeleted bool
	BranchKept    string // why the local branch was kept
	RemoteDeleted bool
	RemoteError   error
}

// Describe returns a one-line summary of the result.
func (r RemoveResult) Describe() string {
	if !r.Removed {
		return "kept: " + r.Skipped
	}
	parts := []string{"worktree removed"}
	switch {
	case r.BranchDeleted:
		parts = append(parts, "branch "+r.Branch+" deleted")
	case r.BranchKept != "":
		parts = append(parts, "branch "+r.Branch+" kept ("+r.BranchKept+")")
	}
	switch {
	case r.RemoteDeleted:
		parts = append(parts, "origin/"+r.Branch+" deleted")
	case r.RemoteError != nil:
		parts = append(parts, "origin/"+r.Branch+" not deleted: "+r.RemoteError.Error())
	}
	return strings.Join(parts, ", ")
}

// RemoveMembers removes the worktrees of the given members. It never forces:
// a worktree with uncommitted or untracked changes is skipped, and so is a
// detached worktree whose HEAD is on no branch.
//
// "Merged" means merged into the branch's base (repo:base) or the default
// branch, squash merges included. Origin is checked first (one ls-remote per
// repository) so stale remote-tracking refs are never trusted.
//
// A local branch is deleted only when it is merged, or pushed to an
// origin/<branch> that is kept. With deleteRemote, origin/<branch> is
// deleted only when it is merged, so open pull requests are never closed;
// the default branch is never deleted.
func (m Manager) RemoveMembers(ctx context.Context, members []Member, deleteRemote bool) []RemoveResult {
	results := make([]RemoveResult, len(members))
	for i, member := range members {
		results[i] = m.removeMember(ctx, member, deleteRemote)
	}
	return results
}

func (m Manager) removeMember(ctx context.Context, member Member, deleteRemote bool) RemoveResult {
	res := RemoveResult{Repo: member.Repo, Branch: member.Branch}
	if member.Broken || !m.Index.Exists(member.Repo) {
		res.Skipped = errMissingRepo.Error()
		return res
	}
	bare := m.Index.BarePath(member.Repo)
	if member.Branch == "" && !headOnBranch(ctx, member.Path) {
		res.Skipped = "detached HEAD has commits not on any branch; create a branch for them in " + member.Path
		return res
	}
	if _, err := git.Run(ctx, bare, "worktree", "remove", member.Path); err != nil {
		reason, _, _ := strings.Cut(err.Error(), "\n")
		res.Skipped = "has uncommitted changes or is locked; inspect it with: git -C " + member.Path + " status (" + reason + ")"
		return res
	}
	res.Removed = true
	if member.Branch == "" {
		return res
	}

	def, err := git.DefaultBranch(ctx, bare)
	if err != nil {
		res.BranchKept = err.Error()
		return res
	}
	if member.Branch == def {
		res.BranchKept = "default branch"
		return res
	}
	if holder, _ := git.WorktreeForBranch(ctx, bare, member.Branch); holder != "" {
		res.BranchKept = "checked out at " + holder
		return res
	}

	// Check origin first: a stale origin/<branch> must not count as a copy
	// of the local branch. Without origin, only merges count.
	recorded := git.BranchBase(ctx, bare, member.Branch)
	originOK := m.Index.FetchBranches(ctx, member.Repo, []string{member.Branch, def, recorded}) == nil
	base := baseRef(ctx, bare, member.Branch, def)
	remote := "origin/" + member.Branch
	hasRemote := originOK && git.RemoteBranchExists(ctx, bare, member.Branch)
	// Merged into its base, or into the default branch.
	isMerged := func(ref string) bool {
		merged, _ := git.Merged(ctx, bare, base, ref)
		if !merged && base != "origin/"+def {
			merged, _ = git.Merged(ctx, bare, "origin/"+def, ref)
		}
		return merged
	}

	// origin/<branch> is only deleted once merged, so open pull requests
	// (yours or a teammate's) are never closed by accident.
	removeRemote := false
	if deleteRemote && git.RemoteBranchExists(ctx, bare, member.Branch) {
		switch {
		case !originOK:
			res.RemoteError = fmt.Errorf("could not reach origin")
		case isMerged(remote):
			removeRemote = true
		default:
			res.RemoteError = fmt.Errorf("not merged into %s; delete it yourself if you mean to: git -C %s push origin --delete %s", base, bare, member.Branch)
		}
	}
	// The local branch may only be deleted when its commits are merged, or
	// pushed to an origin/<branch> that is kept.
	pushed := hasRemote && !removeRemote && git.IsAncestor(ctx, bare, member.Branch, remote)
	merged := !pushed && isMerged(member.Branch)
	switch {
	case merged || pushed:
		if _, err := git.Run(ctx, bare, "branch", "-D", member.Branch); err != nil {
			res.BranchKept = err.Error()
		} else {
			res.BranchDeleted = true
		}
	case !originOK:
		res.BranchKept = "could not reach origin to check it was pushed"
	default:
		res.BranchKept = "has commits not pushed or merged into " + base
	}

	if removeRemote {
		if _, err := git.Run(ctx, bare, "push", "-q", "origin", "--delete", member.Branch); err != nil {
			res.RemoteError = err
		} else {
			res.RemoteDeleted = true
		}
	}
	return res
}

// headOnBranch reports whether the HEAD of the worktree at path is contained
// in a local or remote-tracking branch, so removing the worktree loses no
// commits.
func headOnBranch(ctx context.Context, path string) bool {
	out, err := git.Run(ctx, path, "for-each-ref", "--count=1", "--contains", "HEAD", "--format=%(refname)", "refs/heads", "refs/remotes")
	return err == nil && out != ""
}

// RemoveDir deletes the workspace directory if it is empty. Otherwise it
// returns the remaining entries.
func (m Manager) RemoveDir(name string) ([]string, error) {
	dir := m.Path(name)
	if err := os.Remove(dir); err == nil || os.IsNotExist(err) {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	left := make([]string, len(entries))
	for i, e := range entries {
		left[i] = e.Name()
	}
	if len(left) == 0 {
		return nil, fmt.Errorf("cannot remove %s", dir)
	}
	return left, nil
}
