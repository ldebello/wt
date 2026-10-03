package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ldebello/wt/internal/repo"
	"github.com/ldebello/wt/internal/ui"
	"github.com/ldebello/wt/internal/workspace"
)

func newSyncCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync [repo...]",
		Short: "Fetch every repository in parallel and update primary checkouts",
		Long: `Fetch every indexed repository (or only the ones given) in parallel, refresh
origin/HEAD, prune stale worktrees, and move each primary checkout to the
latest origin/<default>. A primary checkout with local changes or commits is
left alone. Workspace worktrees are never touched.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ix, err := app.Index()
			if err != nil {
				return err
			}
			names := slices.Compact(slices.Sorted(slices.Values(args)))
			if len(names) == 0 {
				if names, err = ix.List(); err != nil {
					return err
				}
			}
			for _, n := range names {
				if err := ix.Require(n); err != nil {
					return err
				}
			}
			if len(names) == 0 {
				app.printf("No repositories yet. Add one with: wt clone <url>\n")
				return nil
			}
			app.printf("Syncing %d repositories...\n", len(names))
			messages := make(map[string]string, len(names))
			var mu sync.Mutex
			errs := repo.ForEach(names, func(name string) error {
				msg, err := ix.Sync(cmd.Context(), name)
				mu.Lock()
				messages[name] = msg
				mu.Unlock()
				return err
			})
			tw := tabwriter.NewWriter(app.Out, 0, 4, 2, ' ', 0)
			failed := 0
			for _, n := range names {
				if errs[n] != nil {
					failed++
					fmt.Fprintf(tw, "  %s\terror: %v\n", n, errs[n])
				} else {
					fmt.Fprintf(tw, "  %s\t%s\n", n, messages[n])
				}
			}
			tw.Flush()
			if failed > 0 {
				return fmt.Errorf("%d of %d repositories failed to sync", failed, len(names))
			}
			return nil
		},
	}
	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return app.repoNames(), noFiles
	}
	return cmd
}

func newCleanupCmd(app *App) *cobra.Command {
	var yes, dryRun, noFetch bool
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Find merged workspaces and remove the ones you select",
		Long: `Fetch the repositories used by workspaces, classify every workspace against
its base (repo:base) or origin/<default>, squash merges included, and remove
the ones you select. Fully merged workspaces are preselected. Removal is the
same as 'wt ws remove': nothing is forced and remote branches are never
deleted.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			mgr, err := app.Workspaces()
			if err != nil {
				return err
			}
			list, err := mgr.List()
			if err != nil {
				return err
			}
			if len(list) == 0 {
				app.printf("No workspaces.\n")
				return nil
			}

			if !noFetch {
				repos := usedRepos(mgr, list)
				app.printf("Fetching %d repositories...\n", len(repos))
				for name, err := range mgr.Index.Fetch(ctx, repos) {
					if err != nil {
						app.warnf("fetch %s: %v\n", name, err)
					}
				}
			}

			statuses := workspaceStatuses(ctx, mgr, list)
			options := make([]ui.Option, len(list))
			var safe []string
			tw := tabwriter.NewWriter(app.Out, 0, 4, 2, ' ', 0)
			app.printf("\nWorkspaces:\n")
			for i, ws := range list {
				label, ok := statuses[i].Summary()
				desc := fmt.Sprintf("%s (%s)", ws.Name, strings.Join(ws.RepoNames(), ", "))
				mark := "[ ]"
				if ok {
					mark = "[x]"
					safe = append(safe, ws.Name)
				}
				fmt.Fprintf(tw, "  %s %s\t-> %s\n", mark, desc, label)
				options[i] = ui.Option{Label: desc + "  -> " + label, Value: ws.Name, Selected: ok}
			}
			tw.Flush()
			if dryRun {
				return nil
			}

			selected := safe
			if !yes {
				picked, err := app.UI.MultiSelect("Workspaces to remove", options)
				selected = ui.Values(picked)
				if errors.Is(err, ui.ErrNoTTY) {
					return errors.New("selection needs a terminal; pass --yes to remove the merged workspaces, or --dry-run")
				}
				if err != nil {
					return err
				}
			}
			if len(selected) == 0 {
				app.printf("Nothing to remove.\n")
				return nil
			}
			if !yes {
				ok, err := app.UI.Confirm(fmt.Sprintf("Remove %d workspaces and their worktrees?", len(selected)), true)
				if err != nil {
					return err
				}
				if !ok {
					return ui.ErrAborted
				}
			}
			for _, name := range selected {
				ws, err := mgr.Load(name)
				if err != nil {
					return err
				}
				app.printf("\n%s:\n", name)
				if err := performRemoval(ctx, app, mgr, ws, ws.Members, false, false); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "remove the merged workspaces without asking")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "only show the classification")
	cmd.Flags().BoolVar(&noFetch, "no-fetch", false, "use the current remote-tracking refs instead of fetching")
	return cmd
}

// workspaceStatuses inspects the workspaces in parallel (each status runs
// several git commands per member), in the order of list.
func workspaceStatuses(ctx context.Context, mgr workspace.Manager, list []workspace.Workspace) []workspace.Status {
	byName := make(map[string]workspace.Status, len(list))
	names := make([]string, len(list))
	index := make(map[string]int, len(list))
	for i, ws := range list {
		names[i] = ws.Name
		index[ws.Name] = i
	}
	var mu sync.Mutex
	repo.ForEach(names, func(name string) error {
		st := mgr.Status(ctx, list[index[name]])
		mu.Lock()
		byName[name] = st
		mu.Unlock()
		return nil
	})
	out := make([]workspace.Status, len(list))
	for i, name := range names {
		out[i] = byName[name]
	}
	return out
}

// usedRepos returns the indexed repositories used by any workspace.
func usedRepos(mgr workspace.Manager, list []workspace.Workspace) []string {
	seen := map[string]bool{}
	var out []string
	for _, ws := range list {
		for _, m := range ws.Members {
			if !seen[m.Repo] && mgr.Index.Exists(m.Repo) {
				seen[m.Repo] = true
				out = append(out, m.Repo)
			}
		}
	}
	sort.Strings(out)
	return out
}
