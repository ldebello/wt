package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ldebello/wt/internal/config"
	"github.com/ldebello/wt/internal/integration"
	"github.com/ldebello/wt/internal/workspace"
)

func newIntegrationsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "integrations",
		Short: "List integrations, or enable/disable one",
		Long: `Integrations run when a workspace is created or its repositories change:

  codegraph          index the whole workspace with CodeGraph
  harness claude     generate CLAUDE.md at the workspace root
  harness generic    generate AGENTS.md at the workspace root

Generated files start with a marker line; delete it to keep your edits.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return listIntegrations(app)
		},
	}

	var disableCodegraph bool
	codegraph := &cobra.Command{
		Use:   "codegraph",
		Short: "Enable (or --disable) CodeGraph indexing of workspaces",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return toggleIntegration(cmd.Context(), app, "codegraph", !disableCodegraph, func(cfg *config.Config, on bool) {
				cfg.Integrations.Codegraph.Enabled = on
			})
		},
	}
	codegraph.Flags().BoolVar(&disableCodegraph, "disable", false, "disable instead of enable")

	var disableHarness bool
	var tmpl, file string
	harness := &cobra.Command{
		Use:   "harness <claude|generic|name>",
		Short: "Enable (or --disable) an AI agent instruction file in workspaces",
		Example: `  wt integrations harness claude
  wt integrations harness cursor --file RULES.md --template agents`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"claude", "generic"},
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			return toggleIntegration(cmd.Context(), app, "harness "+name, !disableHarness, func(cfg *config.Config, on bool) {
				h := cfg.Integrations.Harness[name]
				h.Enabled = on
				if tmpl != "" {
					h.Template = tmpl
				}
				if file != "" {
					h.File = file
				}
				cfg.Integrations.Harness[name] = h
			})
		},
	}
	harness.Flags().BoolVar(&disableHarness, "disable", false, "disable instead of enable")
	harness.Flags().StringVar(&tmpl, "template", "", "template name (~/.wt/templates/<name>.md or built-in claude/agents)")
	harness.Flags().StringVar(&file, "file", "", "generated file name (required for custom harnesses)")

	cmd.AddCommand(codegraph, harness)
	return cmd
}

func listIntegrations(app *App) error {
	list, err := app.integrations()
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(app.Out, 0, 4, 2, ' ', 0)
	for _, i := range list {
		state := "disabled"
		if i.Enabled() {
			state = "enabled"
		}
		detail := ""
		if h, ok := i.(*integration.Harness); ok {
			detail = h.File()
		}
		if i.Enabled() {
			if err := i.Check(); err != nil {
				detail = "problem: " + strings.SplitN(err.Error(), "\n", 2)[0] + " (see wt doctor)"
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", i.Name(), state, detail)
	}
	return tw.Flush()
}

// toggleIntegration applies set to the configuration, checks the integration
// before enabling it, then saves and runs its setup.
func toggleIntegration(ctx context.Context, app *App, name string, on bool, set func(*config.Config, bool)) error {
	cfg, err := app.Config()
	if err != nil {
		return err
	}
	set(cfg, on)
	var target integration.Integration
	for _, i := range integration.All(cfg, app.Home, app.Out) {
		if i.Name() == name {
			target = i
		}
	}
	if target == nil {
		return fmt.Errorf("unknown integration %q", name)
	}
	if on {
		if err := target.Check(); err != nil {
			return fmt.Errorf("not enabled: %w", err)
		}
	}
	if err := app.SaveConfig(); err != nil {
		return err
	}
	if !on {
		app.printf("Disabled %s.\n", name)
		return nil
	}
	if err := target.Setup(ctx); err != nil {
		return err
	}
	app.printf("Enabled %s. It runs when workspaces are created or their repositories change.\n", name)
	return nil
}

func (a *App) integrations() ([]integration.Integration, error) {
	cfg, err := a.Config()
	if err != nil {
		return nil, err
	}
	return integration.All(cfg, a.Home, a.Out), nil
}

func toIntegrationWorkspace(ws workspace.Workspace) integration.Workspace {
	out := integration.Workspace{Name: ws.Name, Path: ws.Path}
	for _, m := range ws.Members {
		out.Repos = append(out.Repos, integration.Repo{Name: m.Repo, Branch: m.Branch, Path: m.Path})
	}
	return out
}

// runWorkspaceHooks runs enabled integrations after a workspace changed.
// Failures are warnings: the workspace itself is ready.
func runWorkspaceHooks(ctx context.Context, app *App, ws workspace.Workspace) error {
	list, err := app.integrations()
	if err != nil {
		return err
	}
	iws := toIntegrationWorkspace(ws)
	for _, i := range list {
		if !i.Enabled() {
			continue
		}
		if err := i.OnWorkspaceCreated(ctx, iws); err != nil {
			app.warnf("%s: %v\n", i.Name(), err)
		}
	}
	return nil
}

// cleanupWorkspaceHooks lets every integration (enabled or not) delete the
// files it generated before the workspace directory is removed.
func cleanupWorkspaceHooks(ctx context.Context, app *App, ws workspace.Workspace) error {
	list, err := app.integrations()
	if err != nil {
		return err
	}
	iws := toIntegrationWorkspace(ws)
	var errs []error
	for _, i := range list {
		if err := i.OnWorkspaceRemoved(ctx, iws); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", i.Name(), err))
		}
	}
	return errors.Join(errs...)
}
