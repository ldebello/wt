package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Shell is the user's shell ($SHELL) when it is sh, bash or zsh, else
// /bin/sh. Scripts run with it non-interactively, which is fast.
func Shell() string {
	sh := os.Getenv("SHELL")
	switch filepath.Base(sh) {
	case "sh", "bash", "zsh":
		return sh
	}
	return "/bin/sh"
}

// HasAliases reports whether aliases can be read from shell.
func HasAliases(shell string) bool {
	base := filepath.Base(shell)
	return base == "zsh" || base == "bash"
}

// LoadAliases asks an interactive shell for its aliases (this loads the
// user's rc files, so it takes a moment).
func LoadAliases(ctx context.Context, shell string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-ic", "alias")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("reading aliases from %s: %w", shell, err)
	}
	return ParseAliases(out.String()), nil
}

// Prepared is a script ready to run.
type Prepared struct {
	Script string
	// Interactive is set when the script needs the user's interactive shell
	// (shell functions that cannot be expanded); it then starts slower.
	Interactive bool
	// Unresolved lists the commands that are not programs, builtins or
	// aliases.
	Unresolved []string
}

// Prepare expands the aliases in script. Aliases are only loaded when the
// script uses a command that is not a program or builtin, so plain scripts
// start instantly.
func Prepare(ctx context.Context, shell, script string) (Prepared, error) {
	_, words := Expand(script, nil)
	if len(Unresolved(words)) == 0 || !HasAliases(shell) {
		return Prepared{Script: script, Unresolved: Unresolved(words)}, nil
	}
	aliases, err := LoadAliases(ctx, shell)
	if err != nil {
		return Prepared{}, err
	}
	expanded, words := Expand(script, aliases)
	unresolved := Unresolved(words)
	return Prepared{Script: expanded, Interactive: len(unresolved) > 0, Unresolved: unresolved}, nil
}

// Target is a directory to run the script in.
type Target struct {
	Name string
	Dir  string
	Env  []string // extra environment variables
}

// Options control how a script runs.
type Options struct {
	Shell       string
	Interactive bool     // run with -ic (loads rc files) instead of -c
	Args        []string // $1, $2...
	Serial      bool     // one target at a time, with the terminal attached
	In          io.Reader
	Out, Err    io.Writer
}

// Result is the outcome for one target.
type Result struct {
	Name     string
	Err      error
	ExitCode int
}

// Run runs script in every target and returns one result per target, in
// order. In parallel mode each target's output is printed as a block when it
// finishes and no input is available (prompts fail instead of hanging).
func Run(ctx context.Context, script string, targets []Target, opts Options) []Result {
	results := make([]Result, len(targets))
	if opts.Serial {
		for i, t := range targets {
			fmt.Fprintf(opts.Out, "── %s ──\n", t.Name)
			cmd := command(ctx, script, t, opts)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = opts.In, opts.Out, opts.Err
			results[i] = result(t.Name, cmd.Run())
		}
		return results
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var buf bytes.Buffer
			cmd := command(ctx, script, t, opts)
			cmd.Stdout, cmd.Stderr = &buf, &buf
			cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0")
			// Own process group, so cancelling stops the whole script
			// (e.g. a git push started by it).
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
			results[i] = result(t.Name, cmd.Run())

			mu.Lock()
			defer mu.Unlock()
			fmt.Fprintf(opts.Out, "── %s ── %s\n", t.Name, results[i].Status())
			if buf.Len() > 0 {
				opts.Out.Write(buf.Bytes())
				if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
					fmt.Fprintln(opts.Out)
				}
			}
		}()
	}
	wg.Wait()
	return results
}

func command(ctx context.Context, script string, t Target, opts Options) *exec.Cmd {
	flag := "-c"
	if opts.Interactive {
		flag = "-ic"
	}
	// sh -c script $0 $1 $2...: the arguments become $1, $2...
	args := append([]string{flag, script, "wt"}, opts.Args...)
	cmd := exec.CommandContext(ctx, opts.Shell, args...)
	cmd.Dir = t.Dir
	cmd.Env = append(os.Environ(), t.Env...)
	return cmd
}

func result(name string, err error) Result {
	r := Result{Name: name, Err: err}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		r.ExitCode = exitErr.ExitCode()
	}
	return r
}

// Status is "ok", "failed (exit N)" or the error.
func (r Result) Status() string {
	switch {
	case r.Err == nil:
		return "ok"
	case r.ExitCode > 0:
		return fmt.Sprintf("failed (exit %d)", r.ExitCode)
	}
	return "failed: " + r.Err.Error()
}

// Summary describes all results on one line.
func Summary(results []Result) string {
	parts := make([]string, len(results))
	for i, r := range results {
		parts[i] = r.Name + " " + r.Status()
	}
	return strings.Join(parts, " · ")
}
