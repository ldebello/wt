// Package cli wires wt's cobra commands to the domain packages.
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/ldebello/wt/internal/config"
	"github.com/ldebello/wt/internal/ui"
)

// App holds the dependencies shared by all commands. Tests build their own
// App with buffers and a fake Prompter.
type App struct {
	Out    io.Writer
	Err    io.Writer
	UI     ui.Prompter
	Home   string // wt home directory (~/.wt)
	cfg    *config.Config
	cfgErr error
	loaded bool
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

func (a *App) printf(format string, args ...any) {
	fmt.Fprintf(a.Out, format, args...)
}

func (a *App) warnf(format string, args ...any) {
	fmt.Fprintf(a.Err, "Warning: "+format, args...)
}
