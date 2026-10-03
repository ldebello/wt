package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ldebello/wt/internal/ui"
)

// resolveTarget maps a `wt cd` / `wt open` argument to a directory:
//
//	<workspace>/<repo>  a repository inside a workspace
//	<workspace>         the workspace root
//	<repo>              the repository's primary checkout
//
// Workspaces win over repositories with the same name. An empty argument
// opens an interactive picker.
func resolveTarget(app *App, arg string) (string, error) {
	mgr, err := app.Workspaces()
	if err != nil {
		return "", err
	}
	if arg == "" {
		return pickTarget(app)
	}
	// "<workspace>/" (as offered by completion) is the workspace itself.
	if arg = strings.TrimRight(arg, "/"); arg == "" {
		return "", errors.New("no workspace or repository named \"/\"")
	}
	if wsName, repoName, ok := strings.Cut(arg, "/"); ok {
		ws, err := mgr.Load(wsName)
		if err != nil {
			return "", err
		}
		m, ok := ws.Member(repoName)
		if !ok {
			return "", fmt.Errorf("repository %q is not in workspace %s", repoName, wsName)
		}
		return m.Path, nil
	}
	if mgr.Exists(arg) {
		return mgr.Path(arg), nil
	}
	if mgr.Index.Exists(arg) {
		return mgr.Index.PrimaryPath(arg), nil
	}
	return "", fmt.Errorf("no workspace or repository named %q", arg)
}

func pickTarget(app *App) (string, error) {
	mgr, err := app.Workspaces()
	if err != nil {
		return "", err
	}
	list, err := mgr.List()
	if err != nil {
		return "", err
	}
	repos, err := mgr.Index.List()
	if err != nil {
		return "", err
	}
	var options []ui.Option
	for _, ws := range list {
		options = append(options, ui.Option{Label: fmt.Sprintf("%s  (workspace: %s)", ws.Name, strings.Join(ws.RepoNames(), ", ")), Value: ws.Path, Group: "workspaces"})
	}
	for _, r := range repos {
		options = append(options, ui.Option{Label: r + "  (repository)", Value: mgr.Index.PrimaryPath(r), Group: "repositories"})
	}
	if len(options) == 0 {
		return "", errors.New("no workspaces or repositories yet; start with: wt clone <url>")
	}
	return app.UI.Select("Open", options)
}
