package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ldebello/wt/internal/git"
)

// Action is what applying a Step does.
type Action int

const (
	// Reuse leaves an existing member untouched.
	Reuse Action = iota
	// CheckoutLocal checks out an existing local branch.
	CheckoutLocal
	// TrackRemote creates a local branch tracking origin/<branch>.
	TrackRemote
	// CreateBranch creates a new branch from Start (not tracking it).
	CreateBranch
)

// Step is the planned change for one repository.
type Step struct {
	Repo   string
	Branch string
	Path   string
	Action Action
	Start  string // start point for CreateBranch
	Note   string // extra information (e.g. a requested branch was ignored)
}

// Describe returns a short human-readable description of the step.
func (s Step) Describe() string {
	var text string
	switch s.Action {
	case Reuse:
		text = fmt.Sprintf("already present on %s, left untouched", branchOrDetached(s.Branch))
	case CheckoutLocal:
		text = "existing branch " + s.Branch
	case TrackRemote:
		text = fmt.Sprintf("branch %s tracking origin/%s", s.Branch, s.Branch)
	case CreateBranch:
		text = fmt.Sprintf("new branch %s from %s", s.Branch, s.Start)
	}
	if s.Note != "" {
		text += " (" + s.Note + ")"
	}
	return text
}

func branchOrDetached(branch string) string {
	if branch == "" {
		return "a detached HEAD"
	}
	return "branch " + branch
}

// Options tunes workspace creation.
type Options struct {
	// From is the base for newly created branches instead of the default
	// branch. A local branch is preferred over origin/<From>.
	From string
}

// Plan resolves specs for workspace name without changing anything. All
// problems are reported together so nothing is created when any repo is
// invalid. Specs without a branch use the workspace name as branch:
// an existing local branch, else origin/<branch>, else a new branch from
// origin/<default> (or Options.From).
func (m Manager) Plan(ctx context.Context, name string, specs []Spec, opts Options) ([]Step, error) {
	existing := Workspace{Name: name, Path: m.Path(name)}
	if m.Exists(name) {
		var err error
		if existing, err = m.Load(name); err != nil {
			return nil, err
		}
	}
	var steps []Step
	var errs []error
	seen := map[string]bool{}
	for _, spec := range specs {
		if seen[spec.Repo] {
			errs = append(errs, fmt.Errorf("%s: listed more than once", spec.Repo))
			continue
		}
		seen[spec.Repo] = true
		step, err := m.planOne(ctx, existing, spec, opts)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", spec.Repo, err))
			continue
		}
		steps = append(steps, step)
	}
	return steps, errors.Join(errs...)
}

func (m Manager) planOne(ctx context.Context, ws Workspace, spec Spec, opts Options) (Step, error) {
	if err := m.Index.Require(spec.Repo); err != nil {
		return Step{}, err
	}
	step := Step{Repo: spec.Repo, Branch: spec.Branch, Path: filepath.Join(ws.Path, spec.Repo)}
	if step.Branch == "" {
		step.Branch = ws.Name
	}
	if member, ok := ws.Member(spec.Repo); ok {
		step.Action = Reuse
		if member.Branch != step.Branch {
			step.Note = "requested branch " + step.Branch
		}
		step.Branch = member.Branch
		return step, nil
	}
	if _, err := os.Lstat(step.Path); err == nil {
		return Step{}, fmt.Errorf("%s exists and is not a worktree of %s", step.Path, spec.Repo)
	}
	if !git.ValidBranchName(ctx, step.Branch) {
		return Step{}, fmt.Errorf("invalid branch name %q", step.Branch)
	}

	bare := m.Index.BarePath(spec.Repo)
	holder, err := git.WorktreeForBranch(ctx, bare, step.Branch)
	if err != nil {
		return Step{}, err
	}
	if holder != "" {
		return Step{}, fmt.Errorf("branch %s is already checked out at %s (git allows a branch in only one worktree)", step.Branch, holder)
	}

	switch {
	case git.LocalBranchExists(ctx, bare, step.Branch):
		step.Action = CheckoutLocal
	case git.RemoteBranchExists(ctx, bare, step.Branch):
		step.Action = TrackRemote
	default:
		step.Action = CreateBranch
		if step.Start, err = m.startPoint(ctx, spec.Repo, opts.From); err != nil {
			return Step{}, err
		}
	}
	return step, nil
}

func (m Manager) startPoint(ctx context.Context, repoName, from string) (string, error) {
	bare := m.Index.BarePath(repoName)
	if from == "" {
		def, err := git.DefaultBranch(ctx, bare)
		if err != nil {
			return "", err
		}
		return "origin/" + def, nil
	}
	if git.LocalBranchExists(ctx, bare, from) {
		return from, nil
	}
	if git.RemoteBranchExists(ctx, bare, from) {
		return "origin/" + from, nil
	}
	return "", fmt.Errorf("base branch %q not found locally or on origin", from)
}

// Apply creates the workspace directory and the planned worktrees, in
// parallel across repositories. If any step fails, everything this call
// created is rolled back.
func (m Manager) Apply(ctx context.Context, name string, steps []Step) error {
	dir := m.Path(name)
	newDir := !m.Exists(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	errs := make([]error, len(steps))
	var wg sync.WaitGroup
	for i, step := range steps {
		if step.Action == Reuse {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = m.addWorktree(ctx, step)
		}()
	}
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		for i, step := range steps {
			if step.Action != Reuse {
				m.rollback(step, errs[i] == nil)
			}
		}
		if newDir {
			os.Remove(dir)
		}
		return err
	}
	return nil
}

func (m Manager) addWorktree(ctx context.Context, s Step) error {
	bare := m.Index.BarePath(s.Repo)
	// Forget worktrees whose directories were deleted by hand, so they don't
	// block the branch.
	if _, err := git.Run(ctx, bare, "worktree", "prune"); err != nil {
		return err
	}
	args := []string{"worktree", "add", "-q"}
	switch s.Action {
	case CheckoutLocal:
		args = append(args, s.Path, s.Branch)
	case TrackRemote:
		args = append(args, "--track", "-b", s.Branch, s.Path, "origin/"+s.Branch)
	case CreateBranch:
		// --no-track: don't make the start point the upstream of the new
		// branch; push.autoSetupRemote sets origin/<branch> on first push.
		args = append(args, "--no-track", "-b", s.Branch, s.Path, s.Start)
	}
	if _, err := git.Run(ctx, bare, args...); err != nil {
		return fmt.Errorf("%s: %w", s.Repo, err)
	}
	return nil
}

// rollback undoes a step. It only touches what the step created: the
// worktree (fresh, so --force cannot lose work) and, for new branches, the
// branch itself. A failed `git worktree add` cleans up its own directory.
func (m Manager) rollback(s Step, succeeded bool) {
	ctx := context.Background()
	bare := m.Index.BarePath(s.Repo)
	if succeeded {
		_, _ = git.Run(ctx, bare, "worktree", "remove", "--force", s.Path)
	}
	_, _ = git.Run(ctx, bare, "worktree", "prune")
	if s.Action == TrackRemote || s.Action == CreateBranch {
		if holder, _ := git.WorktreeForBranch(ctx, bare, s.Branch); holder == "" {
			_, _ = git.Run(ctx, bare, "branch", "-D", s.Branch)
		}
	}
}
