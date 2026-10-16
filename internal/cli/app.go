// Package cli wires wt's cobra commands to the domain packages.
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"

	"github.com/ldebello/wt/internal/config"
	"github.com/ldebello/wt/internal/repo"
	"github.com/ldebello/wt/internal/ui"
	"github.com/ldebello/wt/internal/workspace"
)

// App holds the dependencies shared by all commands. Tests build their own
// App with buffers and a fake Prompter.
type App struct {
	Out      io.Writer
	Err      io.Writer
	UI       ui.Prompter
	Home     string // wt home directory (~/.wt)
	cfg      *config.Config
	cfgErr   error
	loaded   bool
	renderer *lipgloss.Renderer
}

// style lazily builds a renderer bound to a.Out, so color follows whether
// Out is actually a terminal (never on in tests, which write to a buffer)
// and NO_COLOR/terminal capabilities are respected.
func (a *App) style() *lipgloss.Renderer {
	if a.renderer == nil {
		a.renderer = lipgloss.NewRenderer(a.Out)
	}
	return a.renderer
}

// NewApp returns an App wired to the real terminal.
func NewApp() *App {
	home, err := config.HomeDir()
	return &App{Out: os.Stdout, Err: os.Stderr, UI: ui.Terminal{}, Home: home, cfgErr: err}
}

// Config lazily loads settings.toml.
func (a *App) Config() (*config.Config, error) {
	if !a.loaded {
		a.loaded = true
		if a.cfgErr == nil {
			a.cfg, a.cfgErr = config.Load(a.Home)
		}
	}
	return a.cfg, a.cfgErr
}

// SaveConfig writes the current configuration back to settings.toml.
func (a *App) SaveConfig() error {
	return config.Save(a.Home, a.cfg)
}

// Index returns the repository index from the configured repos path.
func (a *App) Index() (repo.Index, error) {
	cfg, err := a.Config()
	if err != nil {
		return repo.Index{}, err
	}
	dir, err := cfg.ReposDir()
	return repo.Index{Dir: dir}, err
}

// Workspaces returns the workspace manager from the configured paths.
func (a *App) Workspaces() (workspace.Manager, error) {
	ix, err := a.Index()
	if err != nil {
		return workspace.Manager{}, err
	}
	dir, err := a.cfg.WorkspacesDir()
	return workspace.Manager{Index: ix, Dir: dir}, err
}

func (a *App) printf(format string, args ...any) {
	fmt.Fprintf(a.Out, format, args...)
}

func (a *App) warnf(format string, args ...any) {
	fmt.Fprintf(a.Err, "Warning: "+format, args...)
}
