package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ldebello/wt/internal/git"
	"github.com/ldebello/wt/internal/repo"
	"github.com/ldebello/wt/internal/ui"
	"github.com/ldebello/wt/internal/workspace"
)

// reservedWorkspaceNames collide with `wt workspace` subcommands.
var reservedWorkspaceNames = []string{"list", "remove"}

func newWorkspaceCmd(app *App) *cobra.Command {
	var repos, bundles []string
	var from string
	var fetch bool
	cmd := &cobra.Command{
		Use:     "workspace <name>",
		Aliases: []string{"ws"},
		Short:   "Create a workspace or add repositories to it",
		Long: `Create ~/workspaces/<name>/ or add repositories to it, one git worktree per
repository. Repositories already in the workspace are left untouched.

A repository without @branch uses the workspace name as branch: an existing
local branch, else origin/<name>, else a new branch from origin/<default>
(or --from). Pass --repos or --bundles without a value to pick interactively.`,
		Example: `  wt ws DOM-12345 --repos domino,cws@main
  wt ws DOM-12345 --bundles backend --repos web@feature/foo
  wt ws DOM-12345 --repos              # pick repositories interactively`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			req := createRequest{
				name:        args[0],
				repos:       repos,
				bundles:     bundles,
				pickRepos:   cmd.Flags().Changed("repos") && len(repos) == 0,
				pickBundles: cmd.Flags().Changed("bundles") && len(bundles) == 0,
				opts:        workspace.Options{From: from},
				fetch:       fetch,
			}
			return createWorkspace(cmd.Context(), app, req)
		},
	}
	cmd.Flags().StringSliceVar(&repos, "repos", nil, "repositories to add: repo or repo@branch, comma-separated (no value: pick interactively)")
	cmd.Flags().StringSliceVar(&bundles, "bundles", nil, "bundles to add, comma-separated (no value: pick interactively)")
	cmd.Flags().StringVar(&from, "from", "", "base for newly created branches instead of the default branch")
	cmd.Flags().BoolVar(&fetch, "fetch", false, "fetch the repositories before resolving branches")
	cmd.AddCommand(newWorkspaceListCmd(app), newWorkspaceRemoveCmd(app))
	return cmd
}

type createRequest struct {
	name                   string
	repos, bundles         []string
	pickRepos, pickBundles bool
	opts                   workspace.Options
	fetch                  bool
}

func createWorkspace(ctx context.Context, app *App, req createRequest) error {
	if err := repo.ValidName("workspace", req.name); err != nil {
		return err
	}
	if slices.Contains(reservedWorkspaceNames, req.name) {
		return fmt.Errorf("%q is reserved for 'wt workspace %s'", req.name, req.name)
	}
	mgr, err := app.Workspaces()
	if err != nil {
		return err
	}
	specs, err := resolveSpecs(app, mgr, req)
	if err != nil {
		return err
	}

	if req.fetch && len(specs) > 0 {
		var names []string
		for _, s := range specs {
			if mgr.Index.Exists(s.Repo) {
				names = append(names, s.Repo)
			}
		}
		app.printf("Fetching %d repositories...\n", len(names))
		for name, err := range mgr.Index.Fetch(ctx, names) {
			if err != nil {
				app.warnf("fetch %s: %v\n", name, err)
			}
		}
	}

	steps, err := mgr.Plan(ctx, req.name, specs, req.opts)
	if err != nil {
		return fmt.Errorf("nothing was created:\n%w", err)
	}
	created := !mgr.Exists(req.name)
	if err := mgr.Apply(ctx, req.name, steps); err != nil {
		return fmt.Errorf("workspace creation failed and was rolled back:\n%w", err)
	}

	app.printf("Workspace %s: %s\n", req.name, mgr.Path(req.name))
	if len(steps) == 0 {
		if created {
			app.printf("  (empty; add repositories with: wt ws %s --repos <repo,...>)\n", req.name)
		} else {
			app.printf("  nothing to add\n")
		}
		return nil
	}
	tw := tabwriter.NewWriter(app.Out, 0, 4, 2, ' ', 0)
	changed := created
	for _, s := range steps {
		fmt.Fprintf(tw, "  %s\t%s\n", s.Repo, s.Describe())
		changed = changed || s.Action != workspace.Reuse
	}
	tw.Flush()
	if !changed {
		return nil
	}
	ws, err := mgr.Load(req.name)
	if err != nil {
		return err
	}
	return runWorkspaceHooks(ctx, app, ws)
}

// resolveSpecs turns --repos/--bundles (or interactive picks) into one list.
func resolveSpecs(app *App, mgr workspace.Manager, req createRequest) ([]workspace.Spec, error) {
	cfg, err := app.Config()
	if err != nil {
		return nil, err
	}
	explicit, err := workspace.ParseSpecs(req.repos)
	if err != nil {
		return nil, err
	}
	if req.pickRepos {
		picked, err := pickRepos(app, mgr, req.name)
		if err != nil {
			return nil, err
		}
		for _, name := range picked {
			explicit = append(explicit, workspace.Spec{Repo: name})
		}
	}

	bundleNames := splitList(req.bundles)
	if req.pickBundles {
		names := cfg.BundleNames()
		if len(names) == 0 {
			return nil, errors.New("no bundles defined yet; create one with: wt bundle <name> --repos <repo,...>")
		}
		options := make([]ui.Option, len(names))
		for i, n := range names {
			options[i] = ui.Option{Label: n + "  (" + strings.Join(cfg.Bundles[n].Repos, ", ") + ")", Value: n}
		}
		if bundleNames, err = app.UI.MultiSelect("Bundles", options); err != nil {
			return nil, err
		}
	}
	var bundles []workspace.NamedSpecs
	for _, name := range bundleNames {
		b, ok := cfg.Bundles[name]
		if !ok {
			return nil, fmt.Errorf("unknown bundle %q (see 'wt bundle list')", name)
		}
		specs, err := workspace.ParseSpecs(b.Repos)
		if err != nil {
			return nil, fmt.Errorf("bundle %s: %w", name, err)
		}
		bundles = append(bundles, workspace.NamedSpecs{Name: name, Specs: specs})
	}

	specs, notes := workspace.Combine(bundles, explicit)
	for _, note := range notes {
		app.printf("Note: %s\n", note)
	}
	return specs, nil
}

// pickRepos offers every indexed repository, preselecting the ones already in
// the workspace.
func pickRepos(app *App, mgr workspace.Manager, wsName string) ([]string, error) {
	names, err := mgr.Index.List()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, errors.New("no repositories in the index yet; add one with: wt clone <url>")
	}
	var current workspace.Workspace
	if mgr.Exists(wsName) {
		current, _ = mgr.Load(wsName)
	}
	options := make([]ui.Option, len(names))
	for i, n := range names {
		_, in := current.Member(n)
		options[i] = ui.Option{Label: n, Value: n, Selected: in}
	}
	return app.UI.MultiSelect("Repositories", options)
}

func newWorkspaceListCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List workspaces with their repositories and branches",
		Long:    "List workspaces. Each repository is shown as repo@branch; '*' marks uncommitted changes.",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := app.Workspaces()
			if err != nil {
				return err
			}
			list, err := mgr.List()
			if err != nil {
				return err
			}
			if len(list) == 0 {
				app.printf("No workspaces yet. Create one with: wt ws <name> --repos <repo,...>\n")
				return nil
			}
			tw := tabwriter.NewWriter(app.Out, 0, 4, 2, ' ', 0)
			for _, ws := range list {
				fmt.Fprintf(tw, "%s\t%s\n", ws.Name, describeMembers(cmd.Context(), ws))
			}
			return tw.Flush()
		},
	}
}

func describeMembers(ctx context.Context, ws workspace.Workspace) string {
	if len(ws.Members) == 0 {
		return "(empty)"
	}
	parts := make([]string, len(ws.Members))
	for i, m := range ws.Members {
		label := m.Repo + "@" + m.Branch
		switch {
		case m.Broken:
			label = m.Repo + " (broken)"
		case m.Branch == "":
			label = m.Repo + " (detached)"
		}
		if dirty, _ := git.IsDirty(ctx, m.Path); dirty {
			label += "*"
		}
		parts[i] = label
	}
	return strings.Join(parts, "  ")
}

func newWorkspaceRemoveCmd(app *App) *cobra.Command {
	var repos []string
	var yes, deleteRemote bool
	cmd := &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Remove a workspace, or some of its repositories",
		Long: `Remove the worktrees of a workspace (or only --repos) and then the workspace
directory. Nothing is forced: worktrees with uncommitted changes are kept,
and a local branch is deleted only if it is merged into origin/<default>
(squash merges included) or pushed to origin. Remote branches are deleted
only when confirmed (or with --delete-remote).`,
		Example: `  wt ws remove DOM-12345
  wt ws remove DOM-12345 --repos cws        # drop one repository
  wt ws remove DOM-12345 --repos            # pick repositories interactively`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pick := cmd.Flags().Changed("repos") && len(repos) == 0
			remoteSet := cmd.Flags().Changed("delete-remote")
			return removeWorkspace(cmd.Context(), app, args[0], splitList(repos), pick, yes, deleteRemote, remoteSet)
		},
	}
	cmd.Flags().StringSliceVar(&repos, "repos", nil, "only remove these repositories (no value: pick interactively)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	cmd.Flags().BoolVar(&deleteRemote, "delete-remote", false, "also delete the branches on origin")
	return cmd
}

func removeWorkspace(ctx context.Context, app *App, name string, repos []string, pick, yes, deleteRemote, remoteSet bool) error {
	mgr, err := app.Workspaces()
	if err != nil {
		return err
	}
	ws, err := mgr.Load(name)
	if err != nil {
		return err
	}

	if pick {
		options := make([]ui.Option, len(ws.Members))
		for i, m := range ws.Members {
			options[i] = ui.Option{Label: m.Repo + "@" + m.Branch, Value: m.Repo}
		}
		if repos, err = app.UI.MultiSelect("Repositories to remove", options); err != nil {
			return err
		}
		if len(repos) == 0 {
			return ui.ErrAborted
		}
	}
	targets := ws.Members
	partial := len(repos) > 0
	if partial {
		targets = nil
		for _, r := range repos {
			m, ok := ws.Member(r)
			if !ok {
				return fmt.Errorf("repository %q is not in workspace %s", r, name)
			}
			targets = append(targets, m)
		}
	}

	app.printf("Workspace %s: %s\n", name, ws.Path)
	for _, m := range targets {
		app.printf("  %s@%s\n", m.Repo, m.Branch)
	}
	if !yes {
		title := fmt.Sprintf("Remove workspace %s?", name)
		if partial {
			title = fmt.Sprintf("Remove %d repositories from %s?", len(targets), name)
		}
		ok, err := app.UI.Confirm(title, false)
		if errors.Is(err, ui.ErrNoTTY) {
			return errors.New("confirmation needs a terminal; pass --yes")
		}
		if err != nil {
			return err
		}
		if !ok {
			return ui.ErrAborted
		}
		if !remoteSet && len(targets) > 0 {
			if deleteRemote, err = app.UI.Confirm("Also delete the matching branches on origin?", false); err != nil {
				return err
			}
		}
	}

	results := mgr.RemoveMembers(ctx, targets, deleteRemote)
	tw := tabwriter.NewWriter(app.Out, 0, 4, 2, ' ', 0)
	for _, r := range results {
		fmt.Fprintf(tw, "  %s\t%s\n", r.Repo, r.Describe())
	}
	tw.Flush()

	ws, err = mgr.Load(name)
	if err != nil {
		return err
	}
	if partial && len(ws.Members) > 0 {
		return runWorkspaceHooks(ctx, app, ws)
	}
	if len(ws.Members) > 0 {
		app.printf("Kept %s: some repositories could not be removed (see above).\n", ws.Path)
		return nil
	}
	if err := cleanupWorkspaceHooks(ctx, app, ws); err != nil {
		app.warnf("%v\n", err)
	}
	left, err := mgr.RemoveDir(name)
	if err != nil {
		return err
	}
	if len(left) > 0 {
		app.printf("Kept %s: it still contains %s\n", ws.Path, strings.Join(left, ", "))
		return nil
	}
	app.printf("Removed workspace %s\n", name)
	return nil
}

// splitList flattens comma-separated values and drops empty items.
func splitList(items []string) []string {
	var out []string
	for _, item := range items {
		for _, part := range strings.Split(item, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// runWorkspaceHooks and cleanupWorkspaceHooks are replaced by the
// integrations framework.
func runWorkspaceHooks(ctx context.Context, app *App, ws workspace.Workspace) error { return nil }

func cleanupWorkspaceHooks(ctx context.Context, app *App, ws workspace.Workspace) error { return nil }
