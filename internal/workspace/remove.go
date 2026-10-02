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
// a worktree with uncommitted or untracked changes is skipped. A local branch
// is deleted only when its commits are safe elsewhere (merged into
// origin/<default>, including squash merges, or pushed to origin/<branch>).
// When deleteRemote is set, origin/<branch> is deleted too (never the
// default branch).
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
		res.Skipped = "its repository is missing from the index (see 'wt doctor')"
		return res
	}
	bare := m.Index.BarePath(member.Repo)
	if _, err := git.Run(ctx, bare, "worktree", "remove", member.Path); err != nil {
		res.Skipped = "has uncommitted changes or is locked; inspect it with: git -C " + member.Path + " status"
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
	if safe, why := branchIsSafe(ctx, bare, member.Branch, def); !safe {
		res.BranchKept = why
	} else if _, err := git.Run(ctx, bare, "branch", "-D", member.Branch); err != nil {
		res.BranchKept = err.Error()
	} else {
		res.BranchDeleted = true
	}

	if deleteRemote && git.RemoteBranchExists(ctx, bare, member.Branch) {
		if _, err := git.Run(ctx, bare, "push", "-q", "origin", "--delete", member.Branch); err != nil {
			res.RemoteError = err
		} else {
			res.RemoteDeleted = true
		}
	}
	return res
}

// branchIsSafe reports whether deleting the local branch loses no commits.
func branchIsSafe(ctx context.Context, bare, branch, def string) (bool, string) {
	if git.RemoteBranchExists(ctx, bare, branch) && git.IsAncestor(ctx, bare, branch, "origin/"+branch) {
		return true, ""
	}
	if merged, _ := git.Merged(ctx, bare, "origin/"+def, branch); merged {
		return true, ""
	}
	return false, "has commits not pushed or merged into origin/" + def
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
