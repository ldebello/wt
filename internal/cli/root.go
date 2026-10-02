package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ldebello/wt/internal/ui"
)

// listFlags may be given without a value to open the interactive picker.
var listFlags = map[string]bool{"--repos": true, "--bundles": true}

// NewRootCmd builds a fresh command tree bound to app.
func NewRootCmd(app *App, version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "wt",
		Short: "Multi-repo workspaces on top of git worktrees",
		Long: `wt keeps one bare clone per repository (~/.repos/<repo>.git) plus a primary
checkout, and builds workspaces (~/workspaces/<name>/) out of git worktrees.`,
		Version:           version,
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: false},
	}
	root.SetOut(app.Out)
	root.SetErr(app.Err)
	root.AddCommand(
		newCloneCmd(app),
		newWorkspaceCmd(app),
		newBundleCmd(app),
		newOpenCmd(app),
		newCdCmd(app),
		newResolveCdCmd(app),
		newShellInitCmd(),
		newIntegrationsCmd(app),
		newSyncCmd(app),
		newCleanupCmd(app),
	)
	return root
}

// PrepareArgs rewrites a value-less --repos/--bundles (last argument, or
// followed by another flag) to "--repos=", which commands treat as "open the
// interactive picker". pflag cannot express an optional value that may also be
// given as a separate argument.
func PrepareArgs(args []string) []string {
	if len(args) > 0 && strings.HasPrefix(args[0], "__complete") {
		return args
	}
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = arg
		if listFlags[arg] && (i+1 == len(args) || strings.HasPrefix(args[i+1], "-")) {
			out[i] = arg + "="
		}
	}
	return out
}

// Execute runs wt with the process arguments and returns the exit code.
func Execute(version string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	app := NewApp()
	root := NewRootCmd(app, version)
	root.SetArgs(PrepareArgs(os.Args[1:]))
	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, ui.ErrAborted) {
			fmt.Fprintln(app.Err, "Aborted.")
		} else {
			fmt.Fprintln(app.Err, "Error:", err)
		}
		return 1
	}
	return 0
}
