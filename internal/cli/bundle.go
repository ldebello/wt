package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ldebello/wt/internal/config"
	"github.com/ldebello/wt/internal/repo"
	"github.com/ldebello/wt/internal/ui"
	"github.com/ldebello/wt/internal/workspace"
)

var reservedBundleNames = []string{"list", "remove"}

func newBundleCmd(app *App) *cobra.Command {
	var repos []string
	cmd := &cobra.Command{
		Use:   "bundle <name>",
		Short: "Create, update or show a bundle of repositories",
		Long: `A bundle is a named set of repositories (optionally with a branch) that you
add to workspaces together with 'wt ws <name> --bundles <bundle>'. Bundles
are stored in settings.toml under [bundles.<name>].`,
		Example: `  wt bundle backend --repos domino,cws@main
  wt bundle backend --repos     # pick repositories interactively
  wt bundle backend             # show the bundle`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			name := args[0]
			if !cmd.Flags().Changed("repos") {
				return showBundle(app, name)
			}
			return saveBundle(app, name, repos, len(repos) == 0)
		},
	}
	cmd.Flags().StringSliceVar(&repos, "repos", nil, "repositories in the bundle: repo or repo@branch, comma-separated (no value: pick interactively)")
	cmd.AddCommand(newBundleListCmd(app), newBundleRemoveCmd(app))
	return cmd
}

func showBundle(app *App, name string) error {
	cfg, err := app.Config()
	if err != nil {
		return err
	}
	b, ok := cfg.Bundles[name]
	if !ok {
		return fmt.Errorf("unknown bundle %q; create it with: wt bundle %s --repos <repo,...>", name, name)
	}
	app.printf("%s: %s\n", name, strings.Join(b.Repos, ", "))
	return nil
}

func saveBundle(app *App, name string, items []string, pick bool) error {
	if err := repo.ValidName("bundle", name); err != nil {
		return err
	}
	if slices.Contains(reservedBundleNames, name) {
		return fmt.Errorf("%q is reserved for 'wt bundle %s'", name, name)
	}
	cfg, err := app.Config()
	if err != nil {
		return err
	}
	ix, err := app.Index()
	if err != nil {
		return err
	}

	specs, err := workspace.ParseSpecs(items)
	if err != nil {
		return err
	}
	if pick {
		if specs, err = pickBundleRepos(app, ix, cfg.Bundles[name]); err != nil {
			return err
		}
	}
	if len(specs) == 0 {
		return errors.New("a bundle needs at least one repository")
	}
	seen := map[string]bool{}
	var errs []error
	values := make([]string, len(specs))
	for i, s := range specs {
		if seen[s.Repo] {
			errs = append(errs, fmt.Errorf("%s: listed more than once", s.Repo))
		}
		seen[s.Repo] = true
		if err := ix.Require(s.Repo); err != nil {
			errs = append(errs, err)
		}
		values[i] = s.String()
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}

	_, existed := cfg.Bundles[name]
	cfg.Bundles[name] = config.Bundle{Repos: values}
	if err := app.SaveConfig(); err != nil {
		return err
	}
	verb := "Created"
	if existed {
		verb = "Updated"
	}
	app.printf("%s bundle %s: %s\n", verb, name, strings.Join(values, ", "))
	return nil
}

// pickBundleRepos offers every indexed repository, preselecting the bundle's
// current ones and keeping their pinned branches.
func pickBundleRepos(app *App, ix repo.Index, current config.Bundle) ([]workspace.Spec, error) {
	names, err := ix.List()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, errors.New("no repositories in the index yet; add one with: wt clone <url>")
	}
	currentSpecs, _ := workspace.ParseSpecs(current.Repos)
	pinned := map[string]workspace.Spec{}
	for _, s := range currentSpecs {
		pinned[s.Repo] = s
	}
	options := make([]ui.Option, len(names))
	for i, n := range names {
		label := n
		if s, ok := pinned[n]; ok {
			label = s.String()
		}
		_, selected := pinned[n]
		options[i] = ui.Option{Label: label, Value: n, Selected: selected}
	}
	picked, err := app.UI.MultiSelect("Repositories in bundle", options)
	if err != nil {
		return nil, err
	}
	specs := make([]workspace.Spec, len(picked))
	for i, n := range picked {
		specs[i] = workspace.Spec{Repo: n}
		if s, ok := pinned[n]; ok {
			specs[i] = s
		}
	}
	return specs, nil
}

func newBundleListCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List bundles",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := app.Config()
			if err != nil {
				return err
			}
			names := cfg.BundleNames()
			if len(names) == 0 {
				app.printf("No bundles yet. Create one with: wt bundle <name> --repos <repo,...>\n")
				return nil
			}
			tw := tabwriter.NewWriter(app.Out, 0, 4, 2, ' ', 0)
			for _, n := range names {
				fmt.Fprintf(tw, "%s\t%s\n", n, strings.Join(cfg.Bundles[n].Repos, ", "))
			}
			return tw.Flush()
		},
	}
}

func newBundleRemoveCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Delete a bundle (repositories and workspaces are not touched)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := app.Config()
			if err != nil {
				return err
			}
			if _, ok := cfg.Bundles[args[0]]; !ok {
				return fmt.Errorf("unknown bundle %q", args[0])
			}
			delete(cfg.Bundles, args[0])
			if err := app.SaveConfig(); err != nil {
				return err
			}
			app.printf("Removed bundle %s\n", args[0])
			return nil
		},
	}
}
