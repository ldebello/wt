package workspace

import (
	"context"
	"fmt"
	"strings"

	"github.com/ldebello/wt/internal/git"
)

// MemberStatus describes one member relative to its base: origin/<base> for
// a branch created with repo:base, else origin/<default>.
type MemberStatus struct {
	Member
	Dirty   bool
	Commits int  // commits not in the base
	Merged  bool // it had work, and that work is in the base (squash merges included)
	Pushed  bool // HEAD is contained in origin/<branch>
	// Untouched is set for members with nothing of their own: on the default
	// branch without local commits, or no commits and never pushed.
	Untouched bool
	// Base is origin/<base> (repo:base) or origin/<default>, set whenever
	// Commits/Merged/Pushed do (i.e. Err is nil and Untouched is false).
	Base string
	// Bare is the repository's bare path, for the manual-delete hint when
	// Commits are not pushed or merged.
	Bare string
	Err  error
}

// Describe returns a one-line summary of the status.
func (ms MemberStatus) Describe() string {
	if ms.Err != nil {
		return "error: " + ms.Err.Error()
	}
	var parts []string
	if ms.Dirty {
		parts = append(parts, "uncommitted changes")
	}
	switch {
	case ms.Untouched:
		parts = append(parts, "no changes of its own")
	case ms.Merged:
		parts = append(parts, "merged")
	case ms.Pushed:
		parts = append(parts, "pushed, not merged yet")
	default:
		parts = append(parts, fmt.Sprintf("%d commit(s) ", ms.Commits)+notMergedHint(ms.Base, ms.Bare, ms.Branch))
	}
	return strings.Join(parts, ", ")
}

// Status is a workspace with the status of each member.
type Status struct {
	Workspace
	Members []MemberStatus
}

// Status inspects every member of ws. It uses local refs only: fetch first
// for an up-to-date answer.
func (m Manager) Status(ctx context.Context, ws Workspace) Status {
	st := Status{Workspace: ws}
	for _, member := range ws.Members {
		st.Members = append(st.Members, m.memberStatus(ctx, member))
	}
	return st
}

func (m Manager) memberStatus(ctx context.Context, member Member) MemberStatus {
	ms := MemberStatus{Member: member}
	if member.Broken || !m.Index.Exists(member.Repo) {
		ms.Err = errMissingRepo
		return ms
	}
	ms.Bare = m.Index.BarePath(member.Repo)
	if ms.Dirty, ms.Err = git.IsDirty(ctx, member.Path); ms.Err != nil {
		return ms
	}
	def, err := m.Index.DefaultBranch(ctx, member.Repo)
	if err != nil {
		ms.Err = err
		return ms
	}
	base := "origin/" + def
	if member.Branch != "" {
		base = baseRef(ctx, ms.Bare, member.Branch, def)
	}
	ms.Base = base
	if ms.Commits, ms.Err = git.CountCommits(ctx, member.Path, base, "HEAD"); ms.Err != nil {
		return ms
	}
	switch {
	case member.Branch == def && ms.Commits == 0:
		ms.Untouched = true
	case ms.Commits > 0:
		ms.Merged, _ = git.Merged(ctx, member.Path, base, "HEAD")
	case git.HasUpstream(ctx, member.Path, member.Branch):
		// Pushed at some point and now fully contained in the default
		// branch: merged with a merge commit or fast-forward.
		ms.Merged = true
	default:
		ms.Untouched = true
	}
	if member.Branch != "" && git.RemoteBranchExists(ctx, member.Path, member.Branch) {
		ms.Pushed = git.IsAncestor(ctx, member.Path, "HEAD", "origin/"+member.Branch)
	}
	return ms
}

// Summary classifies the workspace for cleanup. Only fully merged
// workspaces are reported as safe to remove.
func (s Status) Summary() (label string, safe bool) {
	if len(s.Members) == 0 {
		return "empty", false
	}
	allEmpty, unpushed, unmerged := true, false, false
	for _, ms := range s.Members {
		switch {
		case ms.Err != nil:
			return ms.Repo + ": " + ms.Err.Error(), false
		case ms.Dirty:
			return "uncommitted changes in " + ms.Repo, false
		case ms.Untouched:
			continue
		}
		allEmpty = false
		if !ms.Merged {
			unmerged = true
			if !ms.Pushed {
				unpushed = true
			}
		}
	}
	switch {
	case unpushed:
		return "contains unpushed commits", false
	case unmerged:
		return "pushed, not merged yet", false
	case allEmpty:
		return "no commits yet", false
	}
	return "merged (safe to remove)", true
}
