package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ldebello/wt/internal/git"
	"github.com/ldebello/wt/internal/repo"
	"github.com/ldebello/wt/internal/ui"
	"github.com/ldebello/wt/internal/workspace"
)

// reservedWorkspaceNames collide with `wt ws` subcommands.
var reservedWorkspaceNames = []string{"ls", "list", "rm", "remove", "run"}

func newWorkspaceCmd(app *App) *cobra.Command {
	var repos, bundles []string
	var noFetch bool
	cmd := &cobra.Command{
		Use:     "ws <name>",
		Aliases: []string{"workspace"},
		Short:   "Create a workspace or add repositories to it",
		Long: `Create ~/workspaces/<name>/ or add repositories to it, one git worktree per
repository. Repositories already in the workspace are left untouched.

Without --repos or --bundles, pick bundles and repositories interactively
(ctrl+b on a repository chooses its branch). --repos and --bundles take
comma-separated values and can be repeated. Each repository is one of:

  repo          the workspace branch <name>: an existing local branch, else
                origin/<name>, else a new branch from origin/<default>
  repo:base     the workspace branch <name>, created from <base> if it does
                not exist yet (your own branch, e.g. for a PR into <base>)
  repo@branch   work directly on an existing <branch> (e.g. to review it);
                a branch can be checked out in only one workspace at a time

Before resolving branches, wt asks origin for the latest version of the
branches involved (one quick request per repository, in parallel) and
fetches only what changed. An existing local branch that is behind
origin/<branch> is fast-forwarded; one that has diverged is left as is.
--no-fetch skips the network and uses the refs from the last 'wt sync'.`,
		Example: `  wt ws PROJ-123                                           # pick bundles and repositories
  wt ws PROJ-123 --repos billing,worker                    # branch PROJ-123 in both
  wt ws HOTFIX-77 --repos billing:release-2.4,web:develop  # HOTFIX-77 from each base
  wt ws REVIEW-1 --repos billing@feature/foo               # work on feature/foo itself
  wt ws PROJ-123 --bundles backend --repos tools`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			req := createRequest{
				name:    args[0],
				repos:   repos,
				bundles: bundles,
				pick:    !cmd.Flags().Changed("repos") && !cmd.Flags().Changed("bundles"),
				noFetch: noFetch,
			}
			return createWorkspace(cmd.Context(), app, req)
		},
	}
	cmd.Flags().StringSliceVar(&repos, "repos", nil, "repositories to add: repo, repo:base or repo@branch (comma-separated, repeatable)")
	cmd.Flags().StringSliceVar(&bundles, "bundles", nil, "bundles to add (comma-separated, repeatable)")
	cmd.Flags().BoolVar(&noFetch, "no-fetch", false, "don't check origin for newer branches (work offline)")
	cmd.ValidArgsFunction = completeFirstArg(app.workspaceNames)
	registerListCompletion(cmd, "repos", func([]string) []string { return app.repoNames() })
	registerListCompletion(cmd, "bundles", func([]string) []string { return app.bundleNames() })
	cmd.AddCommand(newWorkspaceListCmd(app), newWorkspaceRemoveCmd(app), newWorkspaceRunCmd(app))
	return cmd
}

type createRequest struct {
	name           string
	repos, bundles []string
	pick           bool // no --repos/--bundles: choose interactively
	noFetch        bool
}

func createWorkspace(ctx context.Context, app *App, req createRequest) error {
	if err := repo.ValidName("workspace", req.name); err != nil {
		return err
	}
	if slices.Contains(reservedWorkspaceNames, req.name) {
		return fmt.Errorf("%q is reserved for 'wt ws %s'", req.name, req.name)
	}
	mgr, err := app.Workspaces()
	if err != nil {
		return err
	}
	if req.pick {
		if req.repos, req.bundles, err = pickWorkspaceContents(ctx, app, mgr, req.name); err != nil {
			return err
		}
	}
	specs, err := resolveSpecs(app, req)
	if err != nil {
		return err
	}

	if !req.noFetch {
		fetchBranches(ctx, app, mgr, req.name, specs)
	}

	steps, err := mgr.Plan(ctx, req.name, specs)
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
			app.printf("  (empty; add repositories with: wt ws %s)\n", req.name)
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

// fetchBranches refreshes, for every repository about to be added, the
// branches that resolution depends on: the requested branch, the default
// branch (base for new branches) and the spec's base. Failures are warnings:
// wt then works from the local refs.
func fetchBranches(ctx context.Context, app *App, mgr workspace.Manager, wsName string, specs []workspace.Spec) {
	var current workspace.Workspace
	if mgr.Exists(wsName) {
		current, _ = mgr.Load(wsName)
	}
	branches := map[string][]string{}
	var names []string
	for _, s := range specs {
		if _, in := current.Member(s.Repo); in || !mgr.Index.Exists(s.Repo) {
			continue
		}
		branch := s.Branch
		if branch == "" {
			branch = wsName
		}
		list := []string{branch}
		if def, err := mgr.Index.DefaultBranch(ctx, s.Repo); err == nil {
			list = append(list, def)
		}
		if s.Base != "" {
			list = append(list, s.Base)
		}
		branches[s.Repo] = list
		names = append(names, s.Repo)
	}
	errs := repo.ForEach(names, func(name string) error {
		return mgr.Index.FetchBranches(ctx, name, branches[name])
	})
	for _, name := range names {
		if errs[name] != nil {
			app.warnf("could not check origin for %s, using local refs: %v\n", name, errs[name])
		}
	}
}

// resolveSpecs merges --repos and --bundles into one list.
func resolveSpecs(app *App, req createRequest) ([]workspace.Spec, error) {
	cfg, err := app.Config()
	if err != nil {
		return nil, err
	}
	explicit, err := workspace.ParseSpecs(req.repos)
	if err != nil {
		return nil, err
	}
	var bundles []workspace.NamedSpecs
	for _, name := range splitList(req.bundles) {
		b, ok := cfg.Bundles[name]
		if !ok {
			return nil, fmt.Errorf("unknown bundle %q (see 'wt bundle ls')", name)
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

const (
	bundlePrefix = "bundle:"
	repoPrefix   = "repo:"
)

// pickWorkspaceContents offers bundles and repositories in one list
// (filterable by kind). ChoiceKey picks a specific branch for a repository.
// Repositories already in the workspace are shown selected and are left
// untouched whatever the answer.
func pickWorkspaceContents(ctx context.Context, app *App, mgr workspace.Manager, wsName string) (repos, bundles []string, err error) {
	cfg, err := app.Config()
	if err != nil {
		return nil, nil, err
	}
	names, err := mgr.Index.List()
	if err != nil {
		return nil, nil, err
	}
	if len(names) == 0 {
		return nil, nil, errors.New("no repositories in the index yet; add one with: wt clone <url>")
	}
	var current workspace.Workspace
	if mgr.Exists(wsName) {
		current, _ = mgr.Load(wsName)
	}

	var options []ui.Option
	for _, n := range cfg.BundleNames() {
		options = append(options, ui.Option{
			Label: n + "  (" + strings.Join(cfg.Bundles[n].Repos, ", ") + ")",
			Value: bundlePrefix + n,
			Group: "bundles",
		})
	}
	for _, n := range names {
		o := ui.Option{Label: n, Value: repoPrefix + n, Group: "repositories"}
		if m, in := current.Member(n); in {
			o.Selected, o.Label = true, n+"  (in workspace on "+m.Branch+")"
		} else {
			o.ChoiceName = "branch"
			o.Choices = branchChoices(ctx, mgr.Index, n, wsName, wsName, wsName+"  (workspace branch: existing, or new from the default branch)")
		}
		options = append(options, o)
	}

	title := "Add to workspace " + wsName
	picked, err := app.UI.MultiSelect(title, options)
	if errors.Is(err, ui.ErrNoTTY) {
		return nil, nil, errors.New("no terminal to pick from; pass --repos <repo,...> or --bundles <bundle,...>")
	}
	if err != nil {
		return nil, nil, err
	}
	for _, o := range picked {
		if name, ok := strings.CutPrefix(o.Value, bundlePrefix); ok {
			bundles = append(bundles, name)
		} else if name, ok := strings.CutPrefix(o.Value, repoPrefix); ok {
			if _, in := current.Member(name); !in {
				repos = append(repos, name+o.Choice) // "", "@branch" or ":base"
			}
		}
	}
	return repos, bundles, nil
}

func newWorkspaceListCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
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
			dirty := dirtyMembers(cmd.Context(), list)
			tw := tabwriter.NewWriter(app.Out, 0, 4, 2, ' ', 0)
			for _, ws := range list {
				fmt.Fprintf(tw, "%s\t%s\n", ws.Name, describeMembers(ws, dirty))
			}
			return tw.Flush()
		},
	}
}

// dirtyMembers runs `git status` for every member of every workspace in
// parallel and returns the paths of the ones with uncommitted changes.
func dirtyMembers(ctx context.Context, list []workspace.Workspace) map[string]bool {
	var paths []string
	for _, ws := range list {
		for _, m := range ws.Members {
			paths = append(paths, m.Path)
		}
	}
	dirty := map[string]bool{}
	var mu sync.Mutex
	repo.ForEach(paths, func(path string) error {
		if d, _ := git.IsDirty(ctx, path); d {
			mu.Lock()
			dirty[path] = true
			mu.Unlock()
		}
		return nil
	})
	return dirty
}

func describeMembers(ws workspace.Workspace, dirty map[string]bool) string {
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
		if dirty[m.Path] {
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
		Use:     "rm <name>",
		Aliases: []string{"remove"},
		Short:   "Remove a workspace, or some of its repositories",
		Long: `Remove the worktrees of a workspace (or only --repos) and then the workspace
directory. Nothing is forced: worktrees with uncommitted changes are kept.

"Merged" means merged into the branch's base (repo:base) or the default
branch, squash merges included; origin is checked first, so stale refs are
never trusted. A local branch is deleted only if it is merged or pushed to
origin. When confirmed (or with --delete-remote), merged branches are also
deleted on origin; unmerged ones (open pull requests) are always kept.`,
		Example: `  wt ws rm PROJ-123
  wt ws rm PROJ-123 --repos worker        # drop one repository`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			remoteSet := cmd.Flags().Changed("delete-remote")
			return removeWorkspace(cmd.Context(), app, args[0], splitList(repos), yes, deleteRemote, remoteSet)
		},
	}
	cmd.Flags().StringSliceVar(&repos, "repos", nil, "only remove these repositories (comma-separated, repeatable)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	cmd.Flags().BoolVar(&deleteRemote, "delete-remote", false, "also delete the branches on origin")
	cmd.ValidArgsFunction = completeFirstArg(app.workspaceNames)
	registerListCompletion(cmd, "repos", func(args []string) []string {
		if len(args) == 0 {
			return nil
		}
		return app.workspaceRepos(args[0])
	})
	return cmd
}

func removeWorkspace(ctx context.Context, app *App, name string, repos []string, yes, deleteRemote, remoteSet bool) error {
	mgr, err := app.Workspaces()
	if err != nil {
		return err
	}
	ws, err := mgr.Load(name)
	if err != nil {
		return err
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
			if !slices.Contains(targets, m) {
				targets = append(targets, m)
			}
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
			if deleteRemote, err = app.UI.Confirm("Also delete the merged branches on origin?", false); err != nil {
				return err
			}
		}
	}
	return performRemoval(ctx, app, mgr, ws, targets, partial, deleteRemote)
}

// performRemoval removes targets from ws, then either refreshes the
// integrations (when members remain) or cleans up generated files and
// deletes the workspace directory.
func performRemoval(ctx context.Context, app *App, mgr workspace.Manager, ws workspace.Workspace, targets []workspace.Member, partial, deleteRemote bool) error {
	name := ws.Name
	results := mgr.RemoveMembers(ctx, targets, deleteRemote)
	tw := tabwriter.NewWriter(app.Out, 0, 4, 2, ' ', 0)
	for _, r := range results {
		fmt.Fprintf(tw, "  %s\t%s\n", r.Repo, r.Describe())
	}
	tw.Flush()

	ws, err := mgr.Load(name)
	if err != nil {
		return err
	}
	if len(ws.Members) > 0 {
		if !partial {
			app.printf("Kept %s: some repositories could not be removed (see above).\n", ws.Path)
		}
		// Refresh the generated files for the repositories that remain.
		if slices.ContainsFunc(results, func(r workspace.RemoveResult) bool { return r.Removed }) {
			return runWorkspaceHooks(ctx, app, ws)
		}
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
