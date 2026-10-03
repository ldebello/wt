package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ldebello/wt/internal/config"
)

func newOpenCmd(app *App) *cobra.Command {
	var noFetch bool
	cmd := &cobra.Command{
		Use:   "open [workspace[/repo] | repo]",
		Short: "Open a workspace or a repository's primary checkout in your editor",
		Long: `Open a workspace, a repository inside it (<workspace>/<repo>), or a
repository's primary checkout in the editor from [editor] command in
settings.toml, else $EDITOR, else 'code'. Without an argument, pick
interactively.

Before opening a primary checkout, wt asks origin for the latest default
branch (one quick request) and moves the checkout to it, so you always see
the latest code. A primary checkout with local changes or commits is left
as is. --no-fetch skips this. Workspaces are opened as they are.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeTargets(app),
		RunE: func(cmd *cobra.Command, args []string) error {
			argv, err := editorCommand(app)
			if err != nil {
				return err
			}
			path, err := resolveTarget(app, firstArg(args))
			if err != nil {
				return err
			}
			if !noFetch {
				refreshPrimary(cmd.Context(), app, path)
			}
			editor := exec.CommandContext(cmd.Context(), argv[0], append(argv[1:], path)...)
			editor.Stdin = os.Stdin
			editor.Stdout = app.Out
			editor.Stderr = app.Err
			return editor.Run()
		},
	}
	cmd.Flags().BoolVar(&noFetch, "no-fetch", false, "open the primary checkout as it is, without checking origin")
	return cmd
}

// refreshPrimary updates path first if it is a primary checkout. Problems
// are warnings: the checkout is opened as it is.
func refreshPrimary(ctx context.Context, app *App, path string) {
	ix, err := app.Index()
	if err != nil {
		return
	}
	name := filepath.Base(path)
	if filepath.Dir(path) != filepath.Clean(ix.Dir) || !ix.Exists(name) {
		return // a workspace or a repository inside one
	}
	msg, err := ix.RefreshPrimary(ctx, name)
	if err != nil {
		app.warnf("could not update %s from origin, opening it as it is: %v\n", name, err)
		return
	}
	fmt.Fprintf(app.Err, "%s: %s\n", name, msg)
}

// editorCommand returns the editor argv and checks that the binary exists.
func editorCommand(app *App) ([]string, error) {
	cfg, err := app.Config()
	if err != nil {
		return nil, err
	}
	command := cfg.Editor.Command
	if command == "" {
		command = os.Getenv("EDITOR")
	}
	if command == "" {
		command = "code"
	}
	argv, err := splitCommand(command)
	if err != nil {
		return nil, fmt.Errorf("invalid editor command %q: %w", command, err)
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		return nil, fmt.Errorf("editor binary '%s' was not found in PATH.\n"+
			"Please install VS Code CLI tools or configure [editor] in %s.\n"+
			"(Shell aliases are not visible to wt; e.g. command = \"open -a 'Visual Studio Code'\" works on macOS.)",
			argv[0], config.File(app.Home))
	}
	return argv, nil
}

// splitCommand splits a command line on whitespace, honouring single and
// double quotes.
func splitCommand(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	var quote rune
	inArg := false
	for _, r := range s {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote, inArg = r, true
		case r == ' ' || r == '\t':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if inArg {
		args = append(args, cur.String())
	}
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	return args, nil
}
