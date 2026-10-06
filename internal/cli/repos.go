package cli

import (
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ldebello/wt/internal/git"
	"github.com/ldebello/wt/internal/ui"
)

func newReposCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "repos",
		Aliases: []string{"repo"},
		Short:   "List the repositories in the index",
		Long: `List the repositories added with 'wt clone': their default branch and the
workspaces that use them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := app.Workspaces()
			if err != nil {
				return err
			}
			names, err := mgr.Index.List()
			if err != nil {
				return err
			}
			if len(names) == 0 {
				app.printf("No repositories yet. Add one with: wt clone <url>\n")
				return nil
			}
			list, err := mgr.List()
			if err != nil {
				return err
			}
			usedBy := map[string][]string{}
			for _, ws := range list {
				for _, m := range ws.Members {
					usedBy[m.Repo] = append(usedBy[m.Repo], ws.Name)
				}
			}
			tw := tabwriter.NewWriter(app.Out, 0, 4, 2, ' ', 0)
			fmt.Fprintf(tw, "REPOSITORY\tDEFAULT\tWORKSPACES\n")
			for _, n := range names {
				def, err := git.DefaultBranch(cmd.Context(), mgr.Index.BarePath(n))
				if err != nil {
					def = "?"
				}
				workspaces := "-"
				if ws := usedBy[n]; len(ws) > 0 {
					workspaces = strings.Join(ws, ", ")
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", n, def, workspaces)
			}
			return tw.Flush()
		},
	}
	cmd.AddCommand(newReposRemoveCmd(app))
	return cmd
}

func newReposRemoveCmd(app *App) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "rm <repo>",
		Aliases: []string{"remove"},
		Short:   "Remove a repository from the index",
		Long: `Delete a repository's primary checkout (~/.repos/<repo>) and bare clone
(~/.repos/<repo>.git). Nothing on origin is touched.

wt refuses, and says why, when that could lose work or break something: a
workspace still uses the repository, the primary checkout has uncommitted
changes or commits not on origin, or a local branch has commits not on
origin (for example one kept by 'wt ws rm' because it was not merged).`,
		Example: `  wt repos rm billing
  wt ws rm PROJ-1 --repos billing && wt repos rm billing`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeFirstArg(app.repoNames),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			ix, err := app.Index()
			if err != nil {
				return err
			}
			if err := ix.Require(name); err != nil {
				return err
			}
			if !yes {
				ok, err := app.UI.Confirm(fmt.Sprintf("Remove repository %s (%s and %s)?", name, ix.PrimaryPath(name), ix.BarePath(name)), false)
				if errors.Is(err, ui.ErrNoTTY) {
					return errors.New("confirmation needs a terminal; pass --yes")
				}
				if err != nil {
					return err
				}
				if !ok {
					return ui.ErrAborted
				}
			}
			if err := ix.Remove(cmd.Context(), name); err != nil {
				return err
			}
			app.printf("Removed repository %s\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}
