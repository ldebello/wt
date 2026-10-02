// Package git is a thin wrapper around the git CLI.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Run executes git in dir and returns trimmed stdout. On failure the error
// includes git's stderr.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := command(ctx, dir, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", wrap(args, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Stream executes git in dir, sending stdout and stderr to w (e.g. clone
// progress).
func Stream(ctx context.Context, dir string, w io.Writer, args ...string) error {
	cmd := command(ctx, dir, args...)
	var stderr bytes.Buffer
	cmd.Stdout = w
	cmd.Stderr = io.MultiWriter(w, &stderr)
	if err := cmd.Run(); err != nil {
		return wrap(args, err, stderr.String())
	}
	return nil
}

// OK reports whether the git command exits successfully.
func OK(ctx context.Context, dir string, args ...string) bool {
	_, err := Run(ctx, dir, args...)
	return err == nil
}

func command(ctx context.Context, dir string, args ...string) *exec.Cmd {
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	return exec.CommandContext(ctx, "git", full...)
}

func wrap(args []string, err error, stderr string) error {
	var exitErr *exec.ExitError
	msg := strings.TrimSpace(stderr)
	if errors.As(err, &exitErr) && msg != "" {
		return fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
}
