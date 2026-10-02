package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ldebello/wt/internal/config"
)

func newOpenCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "open [workspace[/repo] | repo]",
		Short: "Open a workspace or a repository's primary checkout in your editor",
		Long: `Open a workspace, a repository inside it (<workspace>/<repo>), or a
repository's primary checkout in the editor from [editor] command in
settings.toml, else $EDITOR, else 'code'. Without an argument, pick
interactively. wt only launches the editor; it never changes the checkout.`,
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
			editor := exec.CommandContext(cmd.Context(), argv[0], append(argv[1:], path)...)
			editor.Stdin = os.Stdin
			editor.Stdout = app.Out
			editor.Stderr = app.Err
			return editor.Run()
		},
	}
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
