package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

// Completion helpers only read directories (no git, no network), so they
// stay fast. Errors simply produce no suggestions.

type completeFunc func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)

const noFiles = cobra.ShellCompDirectiveNoFileComp

func (a *App) repoNames() []string {
	ix, err := a.Index()
	if err != nil {
		return nil
	}
	names, _ := ix.List()
	return names
}

func (a *App) workspaceNames() []string {
	mgr, err := a.Workspaces()
	if err != nil {
		return nil
	}
	names, _ := mgr.Names()
	return names
}

func (a *App) bundleNames() []string {
	cfg, err := a.Config()
	if err != nil {
		return nil
	}
	return cfg.BundleNames()
}

func (a *App) workspaceRepos(name string) []string {
	mgr, err := a.Workspaces()
	if err != nil {
		return nil
	}
	ws, err := mgr.Load(name)
	if err != nil {
		return nil
	}
	return ws.RepoNames()
}

// completeFirstArg completes only the first positional argument.
func completeFirstArg(values func() []string) completeFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, noFiles
		}
		return values(), noFiles
	}
}

// completeTargets completes workspaces, repositories and <workspace>/<repo>.
func completeTargets(app *App) completeFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, noFiles
		}
		if wsName, _, ok := strings.Cut(toComplete, "/"); ok {
			var out []string
			for _, r := range app.workspaceRepos(wsName) {
				out = append(out, wsName+"/"+r)
			}
			return out, noFiles
		}
		var out []string
		for _, ws := range app.workspaceNames() {
			out = append(out, ws, ws+"/")
		}
		out = append(out, app.repoNames()...)
		return out, noFiles | cobra.ShellCompDirectiveNoSpace
	}
}

// completeList completes a comma-separated flag value, suggesting values not
// yet listed. Items with "@branch" or ":base" are left alone.
func completeList(values func(args []string) []string) completeFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		prefix := ""
		if i := strings.LastIndex(toComplete, ","); i >= 0 {
			prefix = toComplete[:i+1]
		}
		if strings.ContainsAny(toComplete[len(prefix):], "@:") {
			return nil, noFiles | cobra.ShellCompDirectiveNoSpace
		}
		listed := map[string]bool{}
		for _, item := range strings.Split(prefix, ",") {
			name, _, _ := strings.Cut(strings.ReplaceAll(item, ":", "@"), "@")
			listed[name] = true
		}
		var out []string
		for _, v := range values(args) {
			if !listed[v] {
				out = append(out, prefix+v)
			}
		}
		return out, noFiles | cobra.ShellCompDirectiveNoSpace
	}
}

func registerListCompletion(cmd *cobra.Command, flag string, values func(args []string) []string) {
	_ = cmd.RegisterFlagCompletionFunc(flag, completeList(values))
}
