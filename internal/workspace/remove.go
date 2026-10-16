package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	BranchForced  bool   // the branch was deleted though not merged or pushed (deleteUnmerged)
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
	case r.BranchForced:
		parts = append(parts, "branch "+r.Branch+" deleted (not merged or pushed anywhere)")
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
// detached worktree whose HEAD is on no branch. The default branch, and a
// branch checked out in another worktree, are never deleted either.
//
// "Merged" means merged into the branch's base (repo:base) or the default
// branch, squash merges included. Origin is checked first (one ls-remote per
// repository) so stale remote-tracking refs are never trusted.
//
// deleteUnmerged governs the local branch once its worktree is gone: with it,
// the branch is deleted even if its commits are not merged or pushed anywhere
// (there is no workspace left to use it from); without it, it is only deleted
// once safe (merged, or pushed to an origin/<branch> that is kept) — the
// cautious, pre-deleteUnmerged default. Either way, origin is untouched by
// this: with deleteRemote, origin/<branch> is deleted only when it is merged,
// so open pull requests are never closed.
func (m Manager) RemoveMembers(ctx context.Context, members []Member, deleteRemote, deleteUnmerged bool) []RemoveResult {
	results := make([]RemoveResult, len(members))
	for i, member := range members {
		results[i] = m.removeMember(ctx, member, deleteRemote, deleteUnmerged)
	}
	return results
}

func (m Manager) removeMember(ctx context.Context, member Member, deleteRemote, deleteUnmerged bool) RemoveResult {
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

	d, err := m.decideBranch(ctx, bare, member, member.Path)
	if err != nil {
		res.BranchKept = err.Error()
		return res
	}
	if d.isDefault {
		res.BranchKept = "default branch"
		return res
	}
	if d.checkedOutAt != "" {
		res.BranchKept = "checked out at " + d.checkedOutAt
		return res
	}

	// origin/<branch> is only deleted once merged, so open pull requests
	// (yours or a teammate's) are never closed by accident.
	removeRemote := false
	if deleteRemote && d.remoteExists {
		switch {
		case !d.originOK:
			res.RemoteError = fmt.Errorf("could not reach origin")
		case d.remoteMerged:
			removeRemote = true
		default:
			res.RemoteError = fmt.Errorf("not merged into %s; delete it yourself if you mean to: git -C %s push origin --delete %s", d.base, bare, member.Branch)
		}
	}
	// The local branch is deleted once it is safe (merged, or pushed to an
	// origin/<branch> that is kept) or, with deleteUnmerged, regardless.
	pushed := d.hasRemote && !removeRemote && d.pushed
	merged := !pushed && d.merged
	switch {
	case merged || pushed:
		if _, err := git.Run(ctx, bare, "branch", "-D", member.Branch); err != nil {
			res.BranchKept = err.Error()
		} else {
			res.BranchDeleted = true
		}
	case deleteUnmerged:
		if _, err := git.Run(ctx, bare, "branch", "-D", member.Branch); err != nil {
			res.BranchKept = err.Error()
		} else {
			res.BranchDeleted = true
			res.BranchForced = true
		}
	case !d.originOK:
		res.BranchKept = "could not reach origin to check it was pushed"
	default:
		res.BranchKept = "has commits " + notMergedHint(d.base, bare, member.Branch)
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

// branchDecision is what removeMember would do to member.Branch, computed
// without deleting anything (no "git branch -D", no "git push --delete").
type branchDecision struct {
	isDefault    bool
	checkedOutAt string // non-empty: the branch is checked out at another worktree
	base         string
	originOK     bool
	remoteExists bool // origin/<branch> exists, regardless of originOK
	hasRemote    bool // originOK && remoteExists
	merged       bool // the local branch is merged into base or the default branch
	remoteMerged bool // origin/<branch> is merged (meaningful only if hasRemote)
	pushed       bool // the local branch's commits are all on origin/<branch> (meaningful only if hasRemote)
}

// decideBranch evaluates member.Branch against its base/default branch and
// origin (one ls-remote per repository, so a stale origin/<branch> never
// counts as a copy of the local branch). ignorePath excludes a worktree (the
// member's own, about to be or already removed) from counting as "checked
// out elsewhere".
//
// "Merged" means merged into the branch's base (repo:base) or the default
// branch, squash merges included.
func (m Manager) decideBranch(ctx context.Context, bare string, member Member, ignorePath string) (branchDecision, error) {
	def, err := git.DefaultBranch(ctx, bare)
	if err != nil {
		return branchDecision{}, err
	}
	if member.Branch == def {
		return branchDecision{isDefault: true}, nil
	}
	if holder, _ := git.WorktreeForBranch(ctx, bare, member.Branch); holder != "" && resolvePath(holder) != resolvePath(ignorePath) {
		return branchDecision{checkedOutAt: holder}, nil
	}

	// Check origin first: a stale origin/<branch> must not count as a copy
	// of the local branch. Without origin, only merges count.
	recorded := git.BranchBase(ctx, bare, member.Branch)
	originOK := m.Index.FetchBranches(ctx, member.Repo, []string{member.Branch, def, recorded}) == nil
	base := baseRef(ctx, bare, member.Branch, def)
	remote := "origin/" + member.Branch
	remoteExists := git.RemoteBranchExists(ctx, bare, member.Branch)
	hasRemote := originOK && remoteExists
	// Merged into its base, or into the default branch.
	isMerged := func(ref string) bool {
		merged, _ := git.Merged(ctx, bare, base, ref)
		if !merged && base != "origin/"+def {
			merged, _ = git.Merged(ctx, bare, "origin/"+def, ref)
		}
		return merged
	}

	d := branchDecision{
		base:         base,
		originOK:     originOK,
		remoteExists: remoteExists,
		hasRemote:    hasRemote,
		merged:       isMerged(member.Branch),
	}
	if hasRemote {
		d.remoteMerged = isMerged(remote)
		d.pushed = git.IsAncestor(ctx, bare, member.Branch, remote)
	}
	return d, nil
}

// BranchStatus previews what RemoveMembers would do to a member's branch,
// without touching anything.
type BranchStatus struct {
	Repo, Branch string
	Safe         bool   // its commits are merged or pushed to a kept origin/<branch>: no data is at risk either way
	Reason       string // why it is not Safe
	// Overridable is whether deleteUnmerged (RemoveMembers) deletes the
	// branch anyway despite !Safe. False only for a hard guard that no
	// setting bypasses: a decideBranch error, or the branch being checked
	// out in another worktree.
	Overridable bool
	// RemoteExists is whether origin/<branch> is there at all. A merge often
	// deletes it already, so asking to delete it again is either a no-op or,
	// worse, confusing; check this first.
	RemoteExists bool
}

// CheckBranches previews the merge status of members' branches, so it can
// warn before the user confirms a removal. Members with no branch (e.g. a
// detached checkout) or whose repository is missing are skipped: there is
// nothing to merge, or RemoveMembers will report that separately.
func (m Manager) CheckBranches(ctx context.Context, members []Member) []BranchStatus {
	var out []BranchStatus
	for _, member := range members {
		if member.Branch == "" || member.Broken || !m.Index.Exists(member.Repo) {
			continue
		}
		bare := m.Index.BarePath(member.Repo)
		status := BranchStatus{Repo: member.Repo, Branch: member.Branch}
		d, err := m.decideBranch(ctx, bare, member, member.Path)
		status.RemoteExists = d.remoteExists
		switch {
		case err != nil:
			status.Reason = err.Error()
		case d.isDefault:
			continue // always kept; nothing to warn about
		case d.checkedOutAt != "":
			status.Reason = "checked out at " + d.checkedOutAt
		case d.merged || d.pushed:
			status.Safe = true
		case !d.originOK:
			status.Reason = "could not reach origin to check it was pushed or merged"
			status.Overridable = true
		default:
			status.Reason = "has commits " + notMergedHint(d.base, bare, member.Branch)
			status.Overridable = true
		}
		out = append(out, status)
	}
	return out
}

// notMergedHint explains that a branch's commits are nowhere else, and how
// to clear it by hand when that's expected: wt only reads git, so it cannot
// tell an abandoned branch (e.g. a closed, unmerged pull request) from one
// you still need.
func notMergedHint(base, bare, branch string) string {
	return fmt.Sprintf("not pushed or merged into %s; if that's expected (e.g. a closed pull request), delete it yourself: git -C %s branch -D %s", base, bare, branch)
}

// resolvePath resolves symlinks for a reliable comparison against paths from
// `git worktree list` (which resolves them too, e.g. on macOS where /tmp is
// a symlink to /private/tmp); a path that doesn't exist is just cleaned.
func resolvePath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
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
