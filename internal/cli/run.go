package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ldebello/wt/internal/config"
	"github.com/ldebello/wt/internal/repo"
	runner "github.com/ldebello/wt/internal/run"
	"github.com/ldebello/wt/internal/workspace"
)

func newWorkspaceRunCmd(app *App) *cobra.Command {
	var wsName, saved string
	var repos []string
	var serial, parallel bool
	cmd := &cobra.Command{
		Use:   "run [command]",
		Short: "Run a command in every repository of a workspace",
		Long: `Run a shell command, or a saved one (-c, see 'wt commands'), in every
repository of the workspace you are in (or -w <workspace>).

Quote the command so that ';', '&&' and '|' reach wt instead of your shell,
or put it after '--'. Aliases work: they are looked up once in your shell,
not once per repository. Each run gets WT_WORKSPACE, WT_REPO and WT_BRANCH.

Repositories run in parallel and each one's output is shown when it
finishes; commands cannot read input then (prompts fail instead of
hanging). --serial runs one repository at a time with your terminal, for
commands that open an editor or ask questions. A saved command runs in the
mode it was saved with unless --serial or --parallel is given. The command
fails if any repository fails.`,
		Example: `  wt ws run 'git status -s'
  wt ws run 'gca -m "wip" && gp' --repos billing,worker
  wt ws run -w PROJ-123 -- make test
  wt ws run -c commit "fix login"          # saved command with one argument`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if serial && parallel {
				return errors.New("use either --serial or --parallel")
			}
			mgr, err := app.Workspaces()
			if err != nil {
				return err
			}
			if wsName == "" {
				if wsName, err = currentWorkspace(mgr); err != nil {
					return err
				}
			}
			ws, err := mgr.Load(wsName)
			if err != nil {
				return err
			}
			targets, err := runTargets(app, ws, splitList(repos))
			if err != nil {
				return err
			}

			shell := runner.Shell()
			opts := runner.Options{Shell: shell, In: os.Stdin, Out: app.Out, Err: app.Err}
			var script string
			if saved != "" {
				cfg, err := app.Config()
				if err != nil {
					return err
				}
				c, ok := cfg.Commands[saved]
				if !ok {
					return fmt.Errorf("unknown command %q (see 'wt commands list')", saved)
				}
				if err := checkArgs(saved, c, args); err != nil {
					return err
				}
				script, opts.Args, opts.Serial = c.Run, args, c.Serial
			} else {
				if len(args) == 0 {
					return errors.New("nothing to run: pass a command, or -c <saved command>")
				}
				script = args[0]
				if len(args) > 1 {
					script = shellJoin(args)
				}
				prepared, err := runner.Prepare(ctx, shell, script)
				if err != nil {
					return err
				}
				if prepared.Interactive {
					app.warnf("%s: not a program or alias; running with your interactive shell (slower)\n", strings.Join(prepared.Unresolved, ", "))
				}
				script, opts.Interactive = prepared.Script, prepared.Interactive
			}
			if serial || parallel {
				opts.Serial = serial
			}

			results := runner.Run(ctx, script, targets, opts)
			app.printf("\n%s\n", runner.Summary(results))
			failed := 0
			for _, r := range results {
				if r.Err != nil {
					failed++
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d repositories failed", failed, len(results))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&wsName, "workspace", "w", "", "workspace to run in (default: the one you are in)")
	cmd.Flags().StringVarP(&saved, "command", "c", "", "run a saved command (see 'wt commands')")
	cmd.Flags().StringSliceVar(&repos, "repos", nil, "only these repositories (comma-separated, repeatable)")
	cmd.Flags().BoolVar(&serial, "serial", false, "one repository at a time, with your terminal")
	cmd.Flags().BoolVar(&parallel, "parallel", false, "all repositories at once (the default for direct commands)")
	_ = cmd.RegisterFlagCompletionFunc("workspace", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return app.workspaceNames(), noFiles
	})
	_ = cmd.RegisterFlagCompletionFunc("command", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return app.commandNames(), noFiles
	})
	registerListCompletion(cmd, "repos", func([]string) []string {
		name, _ := cmd.Flags().GetString("workspace")
		if name == "" {
			mgr, err := app.Workspaces()
			if err != nil {
				return nil
			}
			if name, err = currentWorkspace(mgr); err != nil {
				return nil
			}
		}
		return app.workspaceRepos(name)
	})
	return cmd
}

// currentWorkspace returns the workspace containing the working directory.
func currentWorkspace(mgr workspace.Manager) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	rel, err := filepath.Rel(resolve(mgr.Dir), resolve(cwd))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("not inside a workspace: cd into one or pass -w <workspace>")
	}
	name, _, _ := strings.Cut(rel, string(filepath.Separator))
	return name, nil
}

// runTargets turns the workspace members (or only repos) into run targets.
func runTargets(app *App, ws workspace.Workspace, repos []string) ([]runner.Target, error) {
	members := ws.Members
	if len(repos) > 0 {
		members = nil
		for _, r := range repos {
			m, ok := ws.Member(r)
			if !ok {
				return nil, fmt.Errorf("repository %q is not in workspace %s", r, ws.Name)
			}
			if !slices.ContainsFunc(members, func(x workspace.Member) bool { return x.Repo == r }) {
				members = append(members, m)
			}
		}
	}
	var targets []runner.Target
	for _, m := range members {
		if m.Broken {
			app.warnf("%s: its repository is missing, skipped (see 'wt check')\n", m.Repo)
			continue
		}
		targets = append(targets, runner.Target{Name: m.Repo, Dir: m.Path, Env: []string{
			"WT_WORKSPACE=" + ws.Name, "WT_REPO=" + m.Repo, "WT_BRANCH=" + m.Branch,
		}})
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("workspace %s has no repositories to run in", ws.Name)
	}
	return targets, nil
}

// checkArgs validates the arguments of saved command name before running.
func checkArgs(name string, c config.Command, args []string) error {
	max, variadic := runner.Params(c.Run)
	usage := "usage: wt ws run -c " + commandUsage(name, c)
	switch {
	case len(args) < max:
		return fmt.Errorf("%s needs %d argument(s): %s\n%s", name, max, strings.Join(argNames(c, max), " "), usage)
	case !variadic && len(args) > max:
		return fmt.Errorf("%s takes %d argument(s), got %d\n%s", name, max, len(args), usage)
	}
	return nil
}

func argNames(c config.Command, max int) []string {
	names := make([]string, max)
	for i := range names {
		if i < len(c.Args) && c.Args[i] != "" {
			names[i] = "<" + c.Args[i] + ">"
		} else {
			names[i] = fmt.Sprintf("<arg%d>", i+1)
		}
	}
	return names
}

func commandUsage(name string, c config.Command) string {
	max, variadic := runner.Params(c.Run)
	parts := append([]string{name}, argNames(c, max)...)
	if variadic {
		parts = append(parts, "[args...]")
	}
	return strings.Join(parts, " ")
}

var plainWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellJoin quotes args so the shell sees them as the same words.
func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		if plainWord.MatchString(a) {
			quoted[i] = a
		} else {
			quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(quoted, " ")
}

var reservedCommandNames = []string{"list", "remove"}

func newCommandsCmd(app *App) *cobra.Command {
	var argsNames []string
	var serial bool
	cmd := &cobra.Command{
		Use:     "commands [name [command]]",
		Aliases: []string{"cmd"},
		Short:   "Save, show or list commands for 'wt ws run -c'",
		Long: `Save a command to run in every repository of a workspace with
'wt ws run -c <name>'. Commands are stored in settings.toml under
[commands.<name>].

Aliases are expanded when saving (wt shows the result), so saved commands
start instantly. Shell functions can't be expanded: write those parts with
plain commands. Use $1, $2... for arguments; wt checks they are given
before running anything, and --args names them for usage and errors. A
command runs in parallel unless saved with --serial (for commands that
need your terminal).

Without a command, 'wt commands <name>' shows it and 'wt commands' lists
them all.`,
		Example: `  wt commands commit 'gca -m "$1" && gup && gp' --args message
  wt commands rebase 'git rebase -i origin/main' --serial
  wt commands commit                     # show it
  wt ws run -c commit "fix login"`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch len(args) {
			case 0:
				return listCommands(app)
			case 1:
				return showCommand(app, args[0])
			}
			return saveCommand(cmd, app, args[0], args[1], argsNames, serial)
		},
	}
	cmd.Flags().StringSliceVar(&argsNames, "args", nil, "names of the arguments $1, $2..., comma-separated")
	cmd.Flags().BoolVar(&serial, "serial", false, "run one repository at a time, with your terminal")
	cmd.ValidArgsFunction = completeFirstArg(app.commandNames)

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List saved commands",
		Args:    cobra.NoArgs,
		RunE:    func(*cobra.Command, []string) error { return listCommands(app) },
	}
	remove := &cobra.Command{
		Use:               "remove <name>",
		Aliases:           []string{"rm"},
		Short:             "Delete a saved command",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeFirstArg(app.commandNames),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := app.Config()
			if err != nil {
				return err
			}
			if _, ok := cfg.Commands[args[0]]; !ok {
				return fmt.Errorf("unknown command %q", args[0])
			}
			delete(cfg.Commands, args[0])
			if err := app.SaveConfig(); err != nil {
				return err
			}
			app.printf("Removed command %s\n", args[0])
			return nil
		},
	}
	cmd.AddCommand(list, remove)
	return cmd
}

func saveCommand(cmd *cobra.Command, app *App, name, script string, names []string, serial bool) error {
	if err := repo.ValidName("command", name); err != nil {
		return err
	}
	if slices.Contains(reservedCommandNames, name) {
		return fmt.Errorf("%q is reserved for 'wt commands %s'", name, name)
	}
	cfg, err := app.Config()
	if err != nil {
		return err
	}
	prepared, err := runner.Prepare(cmd.Context(), runner.Shell(), script)
	if err != nil {
		return err
	}
	if len(prepared.Unresolved) > 0 {
		return fmt.Errorf("%s: not a program, builtin or alias. Shell functions can't be saved; write that part with plain commands",
			strings.Join(prepared.Unresolved, ", "))
	}
	max, _ := runner.Params(prepared.Script)
	if len(names) > 0 && len(names) != max {
		return fmt.Errorf("the command uses %d argument(s) ($1..$%d) but --args names %d", max, max, len(names))
	}
	c := config.Command{Run: prepared.Script, Args: names, Serial: serial}
	_, existed := cfg.Commands[name]
	cfg.Commands[name] = c
	if err := app.SaveConfig(); err != nil {
		return err
	}
	verb := "Saved"
	if existed {
		verb = "Updated"
	}
	app.printf("%s command %s (%s):\n  %s\n", verb, commandUsage(name, c), mode(c), c.Run)
	if prepared.Script != script {
		app.printf("Aliases were expanded; this is what runs.\n")
	}
	return nil
}

func showCommand(app *App, name string) error {
	cfg, err := app.Config()
	if err != nil {
		return err
	}
	c, ok := cfg.Commands[name]
	if !ok {
		return fmt.Errorf("unknown command %q; save it with: wt commands %s '<command>'", name, name)
	}
	app.printf("%s (%s):\n  %s\n", commandUsage(name, c), mode(c), c.Run)
	return nil
}

func listCommands(app *App) error {
	cfg, err := app.Config()
	if err != nil {
		return err
	}
	names := cfg.CommandNames()
	if len(names) == 0 {
		app.printf("No saved commands yet. Save one with: wt commands <name> '<command>'\n")
		return nil
	}
	tw := tabwriter.NewWriter(app.Out, 0, 4, 2, ' ', 0)
	for _, n := range names {
		c := cfg.Commands[n]
		fmt.Fprintf(tw, "%s\t%s\t%s\n", commandUsage(n, c), mode(c), c.Run)
	}
	return tw.Flush()
}

func mode(c config.Command) string {
	if c.Serial {
		return "serial"
	}
	return "parallel"
}

func (a *App) commandNames() []string {
	cfg, err := a.Config()
	if err != nil {
		return nil
	}
	return cfg.CommandNames()
}
