// Package config loads and saves ~/.wt/settings.toml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// HomeEnv overrides the wt home directory (default ~/.wt).
const HomeEnv = "WT_HOME"

// Config mirrors settings.toml.
type Config struct {
	Paths        Paths             `toml:"paths"`
	Editor       Editor            `toml:"editor"`
	Integrations Integrations      `toml:"integrations"`
	Bundles      map[string]Bundle `toml:"bundles,omitempty"`
}

type Paths struct {
	Repos      string `toml:"repos"`
	Workspaces string `toml:"workspaces"`
}

type Editor struct {
	// Command is split into arguments shell-style; the target path is appended.
	// Empty means $EDITOR, then "code".
	Command string `toml:"command,omitempty"`
}

type Integrations struct {
	Codegraph Toggle             `toml:"codegraph"`
	Harness   map[string]Harness `toml:"harness"`
}

type Toggle struct {
	Enabled bool `toml:"enabled"`
}

// Harness generates an AI agent instruction file at the workspace root:
// "claude" writes CLAUDE.md, "generic" writes AGENTS.md.
type Harness struct {
	Enabled  bool   `toml:"enabled"`
	Template string `toml:"template,omitempty"` // ~/.wt/templates/<name>.md or a built-in template
}

// Bundle is a named set of repo specs ("repo", "repo@branch" or "repo:base").
type Bundle struct {
	Repos []string `toml:"repos"`
}

// Default returns the configuration used when settings.toml is absent.
func Default() *Config {
	return &Config{
		Paths: Paths{Repos: "~/.repos", Workspaces: "~/workspaces"},
		Integrations: Integrations{
			Harness: map[string]Harness{
				"claude":  {Enabled: true, Template: "claude"},
				"generic": {Enabled: false, Template: "agents"},
			},
		},
		Bundles: map[string]Bundle{},
	}
}

// HomeDir returns $WT_HOME or ~/.wt.
func HomeDir() (string, error) {
	if dir := os.Getenv(HomeEnv); dir != "" {
		return ExpandHome(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".wt"), nil
}

// File returns the settings.toml path inside the wt home directory.
func File(wtHome string) string {
	return filepath.Join(wtHome, "settings.toml")
}

// Load reads settings.toml from wtHome, falling back to defaults for anything
// the file does not set. A missing file is not an error.
func Load(wtHome string) (*Config, error) {
	cfg := Default()
	path := File(wtHome)
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if cfg.Bundles == nil {
		cfg.Bundles = map[string]Bundle{}
	}
	if cfg.Integrations.Harness == nil {
		cfg.Integrations.Harness = map[string]Harness{}
	}
	return cfg, nil
}

// Save writes cfg to settings.toml atomically. Comments in an existing file
// are not preserved.
func Save(wtHome string, cfg *Config) error {
	var buf bytes.Buffer
	buf.WriteString("# wt settings. Managed by wt: comments are not preserved when wt saves this file.\n\n")
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(wtHome, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(wtHome, "settings-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), File(wtHome))
}

// ReposDir is the expanded repository index directory.
func (c *Config) ReposDir() (string, error) { return ExpandHome(c.Paths.Repos) }

// WorkspacesDir is the expanded workspaces directory.
func (c *Config) WorkspacesDir() (string, error) { return ExpandHome(c.Paths.Workspaces) }

// BundleNames returns bundle names sorted alphabetically.
func (c *Config) BundleNames() []string {
	names := make([]string, 0, len(c.Bundles))
	for name := range c.Bundles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ExpandHome expands a leading "~" and makes the path absolute.
func ExpandHome(path string) (string, error) {
	if path == "" {
		return "", errors.New("empty path")
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return filepath.Abs(path)
}
