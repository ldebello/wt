package cli

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// shellInitEnv is exported by the shell integration so `wt check` can tell
// whether it is loaded.
const shellInitEnv = "WT_SHELL_INIT"

const posixWrapper = `# wt shell integration: 'wt cd' changes the current directory.
wt() {
  if [ "$1" = "cd" ]; then
    shift
    local __wt_dir
    __wt_dir="$(command wt __cd "$@")" || return
    builtin cd -- "$__wt_dir"
  else
    command wt "$@"
  fi
}
export WT_SHELL_INIT=%s
`

const fishWrapper = `# wt shell integration: 'wt cd' changes the current directory.
function wt
    if test (count $argv) -gt 0; and test "$argv[1]" = cd
        set -l __wt_dir (command wt __cd $argv[2..-1]); or return
        builtin cd -- $__wt_dir
    else
        command wt $argv
    end
end
set -gx WT_SHELL_INIT fish
`

func newShellInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell-init <zsh|bash|fish>",
		Short: "Print the shell integration (wt cd + completion)",
		Long: `Print a shell function that makes 'wt cd' change the current directory,
followed by the completion script. Add it to your shell profile:

  zsh  (~/.zshrc, after compinit):  eval "$(wt shell-init zsh)"
  bash (~/.bashrc):                  eval "$(wt shell-init bash)"
  fish (~/.config/fish/config.fish): wt shell-init fish | source`,
		ValidArgs: []string{"zsh", "bash", "fish"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			var completion bytes.Buffer
			root := cmd.Root()
			out := cmd.OutOrStdout()
			switch args[0] {
			case "zsh":
				if err := root.GenZshCompletion(&completion); err != nil {
					return err
				}
				fmt.Fprintf(out, posixWrapper, "zsh")
				// The completion script calls compdef, which needs compinit.
				fmt.Fprintf(out, "if (( $+functions[compdef] )); then\n%s\nfi\n", completion.String())
			case "bash":
				if err := root.GenBashCompletionV2(&completion, true); err != nil {
					return err
				}
				fmt.Fprintf(out, posixWrapper, "bash")
				fmt.Fprint(out, completion.String())
			case "fish":
				if err := root.GenFishCompletion(&completion, true); err != nil {
					return err
				}
				fmt.Fprint(out, fishWrapper)
				fmt.Fprint(out, completion.String())
			}
			return nil
		},
	}
}

func newCdCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "cd [workspace[/repo] | repo]",
		Short: "Change directory to a workspace, a repository in it, or a primary checkout",
		Long: `Change directory to a workspace, a repository inside it (<workspace>/<repo>),
or a repository's primary checkout. Without an argument, pick interactively.
Requires the shell integration: eval "$(wt shell-init zsh)".`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeTargets(app),
		RunE: func(cmd *cobra.Command, args []string) error {
			// The shell function intercepts `wt cd`; reaching the binary means
			// it is not installed.
			path, err := resolveTarget(app, firstArg(args))
			if err != nil {
				return err
			}
			return fmt.Errorf("'wt cd' needs the shell integration. Add to your shell profile:\n"+
				"  eval \"$(wt shell-init zsh)\"   # bash: shell-init bash; fish: wt shell-init fish | source\n"+
				"Meanwhile: cd %s", path)
		},
	}
}

// newResolveCdCmd is called by the shell function; it prints the directory.
func newResolveCdCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:               "__cd [target]",
		Hidden:            true,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeTargets(app),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolveTarget(app, firstArg(args))
			if err != nil {
				return err
			}
			if path == "" {
				return errors.New("nothing selected")
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
