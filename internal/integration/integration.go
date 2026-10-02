// Package integration hooks optional tools into the workspace lifecycle
// without touching core workspace logic.
package integration

import (
	"context"
	"io"
	"sort"

	"github.com/ldebello/wt/internal/config"
)

// Workspace is the view of a workspace passed to integrations.
type Workspace struct {
	Name  string
	Path  string
	Repos []Repo
}

// Repo is one repository worktree in a workspace.
type Repo struct {
	Name   string
	Branch string
	Path   string
}

// Integration is an optional feature attached to workspace events.
type Integration interface {
	// Name is how the integration is addressed: "codegraph", "harness claude".
	Name() string
	Enabled() bool
	// Check verifies dependencies (binaries, templates) without side effects.
	Check() error
	// Setup runs once when the integration is enabled.
	Setup(ctx context.Context) error
	// OnWorkspaceCreated runs after a workspace is created or its
	// repositories change.
	OnWorkspaceCreated(ctx context.Context, ws Workspace) error
	// OnWorkspaceRemoved runs before the workspace directory is deleted, to
	// clean up generated files. It runs even when disabled.
	OnWorkspaceRemoved(ctx context.Context, ws Workspace) error
}

// All returns every integration known from cfg: codegraph first, then the
// harnesses sorted by name.
func All(cfg *config.Config, wtHome string, out io.Writer) []Integration {
	list := []Integration{&Codegraph{enabled: cfg.Integrations.Codegraph.Enabled, out: out}}
	names := make([]string, 0, len(cfg.Integrations.Harness))
	for name := range cfg.Integrations.Harness {
		names = append(names, name)
	}
	for name := range builtinHarnesses {
		if _, ok := cfg.Integrations.Harness[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		list = append(list, NewHarness(name, cfg.Integrations.Harness[name], wtHome, cfg.Integrations.Codegraph.Enabled))
	}
	return list
}
