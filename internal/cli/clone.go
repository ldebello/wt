package cli

import (
	"github.com/spf13/cobra"
)

func newCloneCmd(app *App) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "clone <url>",
		Short: "Add a repository to the index (bare clone + primary checkout)",
		Example: `  wt clone git@github.com:cerebrotech/domino.git
  wt clone https://github.com/other-org/domino.git --name other-domino`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ix, err := app.Index()
			if err != nil {
				return err
			}
			c, err := ix.Clone(cmd.Context(), args[0], name, app.Err)
			if err != nil {
				return err
			}
			app.printf("\nRepository %s ready\n", c.Name)
			app.printf("  bare:    %s\n", c.Bare)
			app.printf("  primary: %s (detached at origin/%s)\n", c.Primary, c.DefaultBranch)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "index the repository under this name instead of the one in the URL")
	return cmd
}
