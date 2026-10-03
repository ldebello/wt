// Package repo manages the repository index: one bare clone per repository
// (<dir>/<name>.git) plus a primary checkout (<dir>/<name>) detached at the
// remote default branch.
package repo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ldebello/wt/internal/git"
)

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidName checks that name is usable as a single path segment.
func ValidName(kind, name string) error {
	if !nameRE.MatchString(name) || strings.HasSuffix(name, ".git") {
		return fmt.Errorf("invalid %s name %q: use letters, digits, '.', '_' and '-' (no '/')", kind, name)
	}
	return nil
}

// NameFromURL derives the friendly repository name from a clone URL or path.
func NameFromURL(url string) (string, error) {
	trimmed := strings.TrimRight(url, "/")
	if i := strings.LastIndexAny(trimmed, "/:"); i >= 0 {
		trimmed = trimmed[i+1:]
	}
	name := strings.TrimSuffix(trimmed, ".git")
	if name == "" {
		return "", fmt.Errorf("cannot determine a repository name from %q", url)
	}
	return name, nil
}

// Index is the directory holding bare repositories and primary checkouts.
type Index struct {
	Dir string
}

// BarePath is the canonical bare repository for name.
func (ix Index) BarePath(name string) string { return filepath.Join(ix.Dir, name+".git") }

// PrimaryPath is the primary checkout for name.
func (ix Index) PrimaryPath(name string) string { return filepath.Join(ix.Dir, name) }

// Exists reports whether name is in the index.
func (ix Index) Exists(name string) bool {
	info, err := os.Stat(ix.BarePath(name))
	return err == nil && info.IsDir()
}

// Require returns an actionable error if name is not in the index.
func (ix Index) Require(name string) error {
	if ix.Exists(name) {
		return nil
	}
	return fmt.Errorf("unknown repository %q (add it with 'wt clone <url>')", name)
}

// List returns the names of all indexed repositories, sorted.
func (ix Index) List() ([]string, error) {
	entries, err := os.ReadDir(ix.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), ".git") {
			names = append(names, strings.TrimSuffix(e.Name(), ".git"))
		}
	}
	sort.Strings(names)
	return names, nil
}

// NameForGitDir maps a git common dir (e.g. from a worktree) back to an
// indexed repository name, or "" if it is not part of this index.
// git records worktree paths with symlinks resolved, so when the index
// directory is reached through a symlink both sides are resolved before
// comparing.
func (ix Index) NameForGitDir(commonDir string) string {
	if !strings.HasSuffix(commonDir, ".git") {
		return ""
	}
	if filepath.Dir(commonDir) != filepath.Clean(ix.Dir) {
		dir, err := filepath.EvalSymlinks(ix.Dir)
		if err != nil {
			return ""
		}
		resolved, err := filepath.EvalSymlinks(commonDir)
		if err != nil || filepath.Dir(resolved) != dir {
			return ""
		}
	}
	return strings.TrimSuffix(filepath.Base(commonDir), ".git")
}

// DefaultBranch returns the remote default branch of name.
func (ix Index) DefaultBranch(ctx context.Context, name string) (string, error) {
	return git.DefaultBranch(ctx, ix.BarePath(name))
}

// Cloned describes a freshly cloned repository.
type Cloned struct {
	Name          string
	Bare          string
	Primary       string
	DefaultBranch string
}

// Clone adds url to the index under name (derived from the URL when empty):
// a bare repository with remote-tracking refs plus a primary checkout detached
// at origin/<default>. Anything created is removed again on failure.
func (ix Index) Clone(ctx context.Context, url, name string, out io.Writer) (_ Cloned, err error) {
	if name == "" {
		if name, err = NameFromURL(url); err != nil {
			return Cloned{}, err
		}
	}
	if err := ValidName("repository", name); err != nil {
		return Cloned{}, fmt.Errorf("%w; pass --name <alias>", err)
	}
	c := Cloned{Name: name, Bare: ix.BarePath(name), Primary: ix.PrimaryPath(name)}
	for _, p := range []string{c.Bare, c.Primary} {
		if _, statErr := os.Stat(p); statErr == nil {
			return Cloned{}, fmt.Errorf("repository %q already exists (%s)\nTo add it under a different name: wt clone %s --name <alias>", name, p, url)
		}
	}
	if err := os.MkdirAll(ix.Dir, 0o755); err != nil {
		return Cloned{}, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(c.Primary)
			os.RemoveAll(c.Bare)
		}
	}()

	// init + fetch (rather than `clone --bare`) so branches land as
	// remote-tracking refs (refs/remotes/origin/*) instead of local branches.
	steps := [][]string{
		{"init", "-q", "--bare", c.Bare},
		{"-C", c.Bare, "remote", "add", "origin", url},
		// First `git push` from a workspace creates and tracks origin/<branch>.
		{"-C", c.Bare, "config", "push.autoSetupRemote", "true"},
	}
	for _, args := range steps {
		if _, err := git.Run(ctx, "", args...); err != nil {
			return Cloned{}, err
		}
	}
	fmt.Fprintf(out, "Fetching %s...\n", url)
	if err := git.Stream(ctx, c.Bare, out, "fetch", "--prune", "origin"); err != nil {
		return Cloned{}, err
	}
	if !git.RefExists(ctx, c.Bare, "refs/remotes/origin/HEAD") {
		// Older git versions do not create origin/HEAD on fetch.
		_, _ = git.Run(ctx, c.Bare, "remote", "set-head", "origin", "--auto")
	}
	if c.DefaultBranch, err = git.DefaultBranch(ctx, c.Bare); err != nil {
		return Cloned{}, err
	}
	if !git.RefExists(ctx, c.Bare, "refs/remotes/origin/HEAD") {
		if _, err := git.Run(ctx, c.Bare, "remote", "set-head", "origin", c.DefaultBranch); err != nil {
			return Cloned{}, err
		}
	}
	if _, err := git.Run(ctx, c.Bare, "worktree", "add", "-q", "--detach", c.Primary, "origin/"+c.DefaultBranch); err != nil {
		return Cloned{}, err
	}
	return c, nil
}
