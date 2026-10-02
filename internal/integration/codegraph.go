package integration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Codegraph indexes the whole workspace root, so the knowledge graph spans
// every repository in it.
type Codegraph struct {
	enabled bool
	out     io.Writer
}

func (c *Codegraph) Name() string  { return "codegraph" }
func (c *Codegraph) Enabled() bool { return c.enabled }

func (c *Codegraph) Check() error {
	if _, err := exec.LookPath("codegraph"); err != nil {
		return errors.New("'codegraph' was not found in PATH; install it from https://github.com/colbymchenry/codegraph")
	}
	return nil
}

func (c *Codegraph) Setup(ctx context.Context) error { return c.Check() }

// OnWorkspaceCreated runs `codegraph init` the first time and an incremental
// `codegraph sync` afterwards. It runs in the foreground.
func (c *Codegraph) OnWorkspaceCreated(ctx context.Context, ws Workspace) error {
	if err := c.Check(); err != nil {
		return err
	}
	args := []string{"init", "--yes", ws.Path}
	if _, err := os.Stat(filepath.Join(ws.Path, ".codegraph")); err == nil {
		args = []string{"sync", ws.Path}
	}
	fmt.Fprintf(c.out, "Running codegraph %s...\n", args[0])
	cmd := exec.CommandContext(ctx, "codegraph", args...)
	cmd.Dir = ws.Path
	cmd.Stdout = c.out
	cmd.Stderr = c.out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("codegraph %s: %w", args[0], err)
	}
	return nil
}

// OnWorkspaceRemoved deletes the workspace index.
func (c *Codegraph) OnWorkspaceRemoved(ctx context.Context, ws Workspace) error {
	return os.RemoveAll(filepath.Join(ws.Path, ".codegraph"))
}
