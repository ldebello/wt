package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ldebello/wt/internal/testutil"
)

func TestResolveTargets(t *testing.T) {
	env := setupRepos(t, "api", "web")
	mustRun(t, env, nil, "ws", "W", "--repos", "api")
	mustRun(t, env, nil, "ws", "web", "--repos", "web") // same name as a repo

	for arg, want := range map[string]string{
		"W":     wsPath(env, "W"),
		"W/api": wsPath(env, "W", "api"),
		"api":   filepath.Join(env.Home, ".repos", "api"),
		"web":   wsPath(env, "web"), // workspaces win
	} {
		r := mustRun(t, env, nil, "__cd", arg)
		if got := strings.TrimSpace(r.out); got != want {
			t.Errorf("__cd %s = %q; want %q", arg, got, want)
		}
	}
	for _, arg := range []string{"nope", "W/web", "X/api"} {
		if r := run(t, env, nil, "__cd", arg); r.e == nil {
			t.Errorf("__cd %s: expected error", arg)
		}
	}

	fake := &fakeUI{selects: []string{wsPath(env, "W")}}
	r := mustRun(t, env, fake, "__cd")
	if strings.TrimSpace(r.out) != wsPath(env, "W") {
		t.Errorf("interactive __cd = %q", r.out)
	}
	if opts := fake.offered[0]; len(opts) != 4 || !strings.Contains(opts[0].Label, "workspace: api") ||
		opts[0].Group != "workspaces" || opts[3].Group != "repositories" {
		t.Errorf("options: %+v", opts)
	}

	r = run(t, env, nil, "cd", "W")
	if r.e == nil || !strings.Contains(r.e.Error(), "shell-init") || !strings.Contains(r.e.Error(), wsPath(env, "W")) {
		t.Errorf("cd without wrapper: %v", r.e)
	}
}

func TestOpen(t *testing.T) {
	env := setupRepos(t, "api")
	log := filepath.Join(env.Root, "editor.log")
	testutil.StubBinary(t, "myeditor", `echo "$@" > `+log)

	// $EDITOR is used when settings.toml has no editor.
	t.Setenv("EDITOR", "myeditor --wait")
	mustRun(t, env, nil, "open", "api")
	if got, _ := os.ReadFile(log); strings.TrimSpace(string(got)) != "--wait "+filepath.Join(env.Home, ".repos", "api") {
		t.Errorf("editor args = %q", got)
	}

	// settings.toml wins over $EDITOR.
	testutil.WriteFile(t, filepath.Join(env.WTHome, "settings.toml"), "[editor]\ncommand = \"myeditor -n\"\n")
	mustRun(t, env, nil, "ws", "W", "--repos", "api")
	mustRun(t, env, nil, "open", "W")
	if got, _ := os.ReadFile(log); strings.TrimSpace(string(got)) != "-n "+wsPath(env, "W") {
		t.Errorf("editor args = %q", got)
	}

	testutil.WriteFile(t, filepath.Join(env.WTHome, "settings.toml"), "[editor]\ncommand = \"missing-editor\"\n")
	r := run(t, env, nil, "open", "W")
	if r.e == nil || !strings.Contains(r.e.Error(), "editor binary 'missing-editor' was not found in PATH") {
		t.Errorf("got %v", r.e)
	}
}

func TestSplitCommand(t *testing.T) {
	cases := map[string][]string{
		"code":                            {"code"},
		"  code   -n ":                    {"code", "-n"},
		`open -a 'Visual Studio Code'`:    {"open", "-a", "Visual Studio Code"},
		`"/Applications/My Editor" --new`: {"/Applications/My Editor", "--new"},
	}
	for in, want := range cases {
		got, err := splitCommand(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("splitCommand(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "   ", `code "unterminated`} {
		if _, err := splitCommand(bad); err == nil {
			t.Errorf("splitCommand(%q): expected error", bad)
		}
	}
}

func completions(t *testing.T, env *testutil.Env, args ...string) []string {
	t.Helper()
	r := mustRun(t, env, nil, append([]string{"__complete"}, args...)...)
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(r.out), "\n") {
		if !strings.HasPrefix(line, ":") && line != "" {
			out = append(out, strings.SplitN(line, "\t", 2)[0])
		}
	}
	return out
}

func TestCompletion(t *testing.T) {
	env := setupRepos(t, "api", "web")
	mustRun(t, env, nil, "ws", "W", "--repos", "api,web")
	mustRun(t, env, nil, "bundle", "backend", "--repos", "api")

	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"cd", ""}, []string{"W", "W/", "api", "web"}},
		{[]string{"open", "W/"}, []string{"W/api", "W/web"}},
		{[]string{"ws", "remove", ""}, []string{"W"}},
		{[]string{"ws", "remove", "W", "--repos", ""}, []string{"api", "web"}},
		{[]string{"ws", "X", "--repos", "api,"}, []string{"api,web"}},
		{[]string{"ws", "X", "--repos", "api@"}, nil},
		{[]string{"ws", "X", "--bundles", ""}, []string{"backend"}},
		{[]string{"bundle", "remove", ""}, []string{"backend"}},
		{[]string{"shell-init", ""}, []string{"zsh", "bash", "fish"}},
	}
	for _, c := range cases {
		if got := completions(t, env, c.args...); !reflect.DeepEqual(got, c.want) {
			t.Errorf("complete %v = %v; want %v", c.args, got, c.want)
		}
	}
}

func TestShellInitOutput(t *testing.T) {
	env := testutil.Setup(t)
	for shell, want := range map[string]string{
		"zsh":  "compdef _wt wt",
		"bash": "__start_wt",
		"fish": "complete -c wt",
	} {
		r := mustRun(t, env, nil, "shell-init", shell)
		if !strings.Contains(r.out, "command wt __cd") || !strings.Contains(r.out, want) || !strings.Contains(r.out, "WT_SHELL_INIT") {
			t.Errorf("shell-init %s output incomplete", shell)
		}
	}
	if r := run(t, env, nil, "shell-init", "tcsh"); r.e == nil {
		t.Error("expected error for unsupported shell")
	}
}

// originalEnv is captured before tests override HOME and friends.
var originalEnv = os.Environ()

// TestShellIntegrationEndToEnd builds the binary and runs `wt cd` in real
// shells.
func TestShellIntegrationEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	env := setupRepos(t, "api")
	mustRun(t, env, nil, "ws", "W", "--repos", "api")

	bin := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "wt"), "github.com/ldebello/wt/cmd/wt")
	build.Env = originalEnv // the test's fake HOME would relocate the module cache
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	scripts := map[string][]string{
		"bash": {"bash", "--noprofile", "--norc", "-c", `eval "$(wt shell-init bash)"; wt cd W/api && pwd`},
		"zsh":  {"zsh", "-f", "-c", `autoload -Uz compinit && compinit -u -D; eval "$(wt shell-init zsh)"; wt cd W/api && pwd`},
		"fish": {"fish", "--no-config", "-c", `wt shell-init fish | source; wt cd W/api; and pwd`},
	}
	for shell, argv := range scripts {
		if _, err := exec.LookPath(shell); err != nil {
			continue
		}
		out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
		if err != nil {
			t.Errorf("%s: %v\n%s", shell, err, out)
			continue
		}
		if got := strings.TrimSpace(string(out)); got != wsPath(env, "W", "api") {
			t.Errorf("%s: pwd = %q", shell, got)
		}
	}
}

func TestOpenRefreshesPrimaryCheckout(t *testing.T) {
	env := testutil.Setup(t)
	up := env.NewUpstream(t, "api", "main")
	mustRun(t, env, nil, "clone", up)
	mustRun(t, env, nil, "ws", "W", "--repos", "api")
	testutil.StubBinary(t, "myeditor", "exit 0")
	t.Setenv("EDITOR", "myeditor")
	primary := filepath.Join(env.Home, ".repos", "api")

	// A new commit upstream: --no-fetch opens the checkout as it is.
	testutil.Commit(t, up, "main", "new.txt", "1")
	mustRun(t, env, nil, "open", "api", "--no-fetch")
	if _, err := os.Stat(filepath.Join(primary, "new.txt")); !os.IsNotExist(err) {
		t.Error("--no-fetch updated the primary checkout")
	}

	// By default it is moved to the latest default branch first.
	r := mustRun(t, env, nil, "open", "api")
	if !strings.Contains(r.err, "api: primary updated to origin/main") {
		t.Errorf("stderr:\n%s", r.err)
	}
	if _, err := os.Stat(filepath.Join(primary, "new.txt")); err != nil {
		t.Error("primary checkout not updated")
	}
	if r = mustRun(t, env, nil, "open", "api"); !strings.Contains(r.err, "primary up to date") {
		t.Errorf("stderr:\n%s", r.err)
	}

	// Local changes are never overwritten.
	testutil.Commit(t, up, "main", "new.txt", "2")
	testutil.WriteFile(t, filepath.Join(primary, "scratch.txt"), "mine")
	if r = mustRun(t, env, nil, "open", "api"); !strings.Contains(r.err, "local changes, not updated") {
		t.Errorf("stderr:\n%s", r.err)
	}

	// Workspaces are opened as they are, without asking origin.
	if r = mustRun(t, env, nil, "open", "W"); r.err != "" {
		t.Errorf("workspace open printed:\n%s", r.err)
	}

	// Origin unreachable: warn and open anyway.
	os.RemoveAll(up)
	if r = mustRun(t, env, nil, "open", "api"); !strings.Contains(r.err, "could not update api from origin, opening it as it is") {
		t.Errorf("stderr:\n%s", r.err)
	}
}

func TestHelpListsAliases(t *testing.T) {
	env := testutil.Setup(t)
	r := mustRun(t, env, nil, "--help")
	for _, want := range []string{"workspace, ws ", "repos, repo ", "commands, cmd "} {
		if !strings.Contains(r.out, want) {
			t.Errorf("root help missing %q:\n%s", want, r.out)
		}
	}
	r = mustRun(t, env, nil, "ws", "--help")
	for _, want := range []string{"list, ls ", "remove, rm "} {
		if !strings.Contains(r.out, want) {
			t.Errorf("ws help missing %q:\n%s", want, r.out)
		}
	}
	// Hidden commands stay hidden.
	if strings.Contains(r.out, "__cd") {
		t.Error("hidden command listed")
	}
}
