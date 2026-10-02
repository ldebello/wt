package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ldebello/wt/internal/doctor"
)

func newDoctorCmd(app *App) *cobra.Command {
	var fix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check git, repositories, workspaces, editor, integrations and shell setup",
		Long: `Run health checks. Problems marked (fixable) can be repaired with --fix:
missing origin/HEAD, fetch refspec, stale worktree registrations and missing
primary checkouts.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			in, err := doctorInputs(app)
			if err != nil {
				return err
			}
			findings := doctor.Run(ctx, in)
			if fix {
				fixed := 0
				for _, f := range findings {
					if !f.Fixable() {
						continue
					}
					if err := f.Fix(ctx); err != nil {
						app.warnf("%s: fix failed: %v\n", f.Area, err)
					} else {
						fixed++
					}
				}
				app.printf("Applied %d fixes.\n\n", fixed)
				findings = doctor.Run(ctx, in)
			}
			return reportFindings(ctx, app, findings)
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "repair fixable problems")
	return cmd
}

func doctorInputs(app *App) (doctor.Inputs, error) {
	if _, err := app.Config(); err != nil {
		return doctor.Inputs{}, fmt.Errorf("settings: %w", err)
	}
	mgr, err := app.Workspaces()
	if err != nil {
		return doctor.Inputs{}, err
	}
	integrations, err := app.integrations()
	if err != nil {
		return doctor.Inputs{}, err
	}
	return doctor.Inputs{
		Index:        mgr.Index,
		Workspaces:   mgr,
		Integrations: integrations,
		Editor:       func() error { _, err := editorCommand(app); return err },
		ShellInit:    os.Getenv(shellInitEnv),
	}, nil
}

func reportFindings(_ context.Context, app *App, findings []doctor.Finding) error {
	tw := tabwriter.NewWriter(app.Out, 0, 4, 2, ' ', 0)
	errors, fixable := 0, 0
	for _, f := range findings {
		msg := f.Message
		if f.Fixable() {
			msg += " (fixable)"
			fixable++
		}
		fmt.Fprintf(tw, "[%s]\t%s\t%s\n", f.Level, f.Area, msg)
		if f.Hint != "" {
			fmt.Fprintf(tw, "\t\t  %s\n", f.Hint)
		}
		if f.Level == doctor.Error {
			errors++
		}
	}
	tw.Flush()
	if fixable > 0 {
		app.printf("\nRun 'wt doctor --fix' to repair %d problem(s).\n", fixable)
	}
	if errors > 0 {
		return fmt.Errorf("%d problem(s) found", errors)
	}
	return nil
}
