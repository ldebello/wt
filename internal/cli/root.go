package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/ldebello/wt/internal/ui"
)

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
		newReposCmd(app),
		newWorkspaceCmd(app),
		newBundleCmd(app),
		newOpenCmd(app),
		newCdCmd(app),
		newResolveCdCmd(app),
		newShellInitCmd(),
		newIntegrationsCmd(app),
		newSyncCmd(app),
		newCleanupCmd(app),
		newCheckCmd(app),
	)
	return root
}

// Execute runs wt with the process arguments and returns the exit code.
func Execute(version string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	app := NewApp()
	root := NewRootCmd(app, version)
	root.SetArgs(os.Args[1:])
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
