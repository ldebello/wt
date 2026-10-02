// Package workspace manages workspaces: directories under the workspaces root
// whose children are git worktrees of indexed repositories.
//
// A workspace has no metadata file. Its repositories are discovered from the
// worktrees it contains, so the filesystem is the single source of truth.
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ldebello/wt/internal/repo"
)

// Workspace is a workspace directory and the repository worktrees in it.
type Workspace struct {
	Name    string
	Path    string
	Members []Member
	// Other lists entries that are not member worktrees (generated files,
	// stray directories).
	Other []string
}

// Member is one repository worktree inside a workspace.
type Member struct {
	Repo   string
	Path   string
	Branch string // empty when detached
	// Broken is set when the worktree's repository no longer exists.
	Broken bool
}

// Member returns the member for repo, if present.
func (w Workspace) Member(repo string) (Member, bool) {
	for _, m := range w.Members {
		if m.Repo == repo {
			return m, true
		}
	}
	return Member{}, false
}

// RepoNames returns the member repository names.
func (w Workspace) RepoNames() []string {
	names := make([]string, len(w.Members))
	for i, m := range w.Members {
		names[i] = m.Repo
	}
	return names
}

// Manager creates, inspects and removes workspaces.
type Manager struct {
	Index repo.Index
	Dir   string // workspaces root
}

// Path returns the directory of workspace name.
func (m Manager) Path(name string) string { return filepath.Join(m.Dir, name) }

// Exists reports whether workspace name exists.
func (m Manager) Exists(name string) bool {
	info, err := os.Stat(m.Path(name))
	return err == nil && info.IsDir()
}

// Names lists workspace names, sorted.
func (m Manager) Names() ([]string, error) {
	entries, err := os.ReadDir(m.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// List loads every workspace.
func (m Manager) List() ([]Workspace, error) {
	names, err := m.Names()
	if err != nil {
		return nil, err
	}
	list := make([]Workspace, 0, len(names))
	for _, name := range names {
		ws, err := m.Load(name)
		if err != nil {
			return nil, err
		}
		list = append(list, ws)
	}
	return list, nil
}

// Load reads workspace name from disk. It only reads files (no git
// subprocesses), so it is cheap enough for shell completion.
func (m Manager) Load(name string) (Workspace, error) {
	ws := Workspace{Name: name, Path: m.Path(name)}
	entries, err := os.ReadDir(ws.Path)
	if errors.Is(err, os.ErrNotExist) {
		return ws, fmt.Errorf("workspace %q does not exist", name)
	}
	if err != nil {
		return ws, err
	}
	for _, e := range entries {
		path := filepath.Join(ws.Path, e.Name())
		if member, ok := m.readMember(path); ok {
			ws.Members = append(ws.Members, member)
		} else {
			ws.Other = append(ws.Other, e.Name())
		}
	}
	return ws, nil
}

// readMember inspects path/.git ("gitdir: <bare>/worktrees/<id>") to find the
// owning repository and checked-out branch.
func (m Manager) readMember(path string) (Member, bool) {
	data, err := os.ReadFile(filepath.Join(path, ".git"))
	if err != nil {
		return Member{}, false
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
	if !ok {
		return Member{}, false
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(path, gitDir)
	}
	commonDir := filepath.Dir(filepath.Dir(gitDir)) // <bare>/worktrees/<id>
	if rel, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		commonDir = filepath.Clean(filepath.Join(gitDir, strings.TrimSpace(string(rel))))
	}
	name := m.Index.NameForGitDir(commonDir)
	if name == "" {
		return Member{}, false
	}
	member := Member{Repo: name, Path: path}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		member.Broken = true
		return member, true
	}
	if ref, ok := strings.CutPrefix(strings.TrimSpace(string(head)), "ref: refs/heads/"); ok {
		member.Branch = ref
	}
	return member, true
}
