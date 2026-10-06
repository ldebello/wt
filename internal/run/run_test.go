package run

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseAliases(t *testing.T) {
	zsh := "gca='git commit --verbose --all'\ngp='git push'\nll='ls -l'\nwho=whoami\nq='echo '\\''hi'\\'''\n"
	bash := "alias gst='git status'\nalias l='ls -CF'\n"
	got := ParseAliases(zsh + bash)
	want := map[string]string{
		"gca": "git commit --verbose --all", "gp": "git push", "ll": "ls -l", "who": "whoami",
		"q": "echo 'hi'", "gst": "git status", "l": "ls -CF",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseAliases = %#v", got)
	}
}

func TestExpand(t *testing.T) {
	aliases := map[string]string{
		"gca": "git commit --verbose --all",
		"gup": "git pull --rebase",
		"gp":  "git push",
		"ls":  "ls -G",     // self-referencing
		"gpb": "gp origin", // alias of alias
		"gbr": "git branch --show-current",
	}
	cases := []struct{ in, want string }{
		{`gca -m "$1" && gup && gp`, `git commit --verbose --all -m "$1" && git pull --rebase && git push`},
		{`gca;gp|gp`, `git commit --verbose --all;git push|git push`},
		{`echo gp 'gp' "gp"`, `echo gp 'gp' "gp"`}, // only command words
		{`'gp' x`, `'gp' x`}, // quoted command: not an alias
		{`ls`, `ls -G`},
		{`gpb`, `git push origin`},
		{`FOO=1 gp`, `FOO=1 git push`},
		{`echo $(gbr) "$(gbr)" ` + "`gbr`", `echo $(git branch --show-current) "$(git branch --show-current)" ` + "`git branch --show-current`"},
		{`if gp; then gup; fi`, `if git push; then git pull --rebase; fi`},
		{`gp > out.txt 2>&1`, `git push > out.txt 2>&1`},
		{`echo $((1 + 2))`, `echo $((1 + 2))`},
	}
	for _, c := range cases {
		if got, _ := Expand(c.in, aliases); got != c.want {
			t.Errorf("Expand(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
	_, words := Expand(`gca -m x && make test | tee log; $(gbr)`, aliases)
	if want := []string{"git", "make", "tee", "git"}; !reflect.DeepEqual(words, want) {
		t.Errorf("words = %v; want %v", words, want)
	}
}

func TestParams(t *testing.T) {
	cases := []struct {
		script   string
		max      int
		variadic bool
	}{
		{`git commit -m "$1"`, 1, false},
		{`echo $2 ${3} $1`, 3, false},
		{`echo ${12}`, 12, false},
		{`echo $10`, 1, false}, // $1 followed by 0
		{`echo '$1' \$2`, 0, false},
		{`git add "$@"`, 0, true},
		{`echo $0 $#`, 0, false},
	}
	for _, c := range cases {
		max, variadic := Params(c.script)
		if max != c.max || variadic != c.variadic {
			t.Errorf("Params(%q) = %d, %v; want %d, %v", c.script, max, variadic, c.max, c.variadic)
		}
	}
}

func TestUnresolved(t *testing.T) {
	got := Unresolved([]string{"sh", "cd", "./script.sh", "$EDITOR", "definitely-not-a-command", "definitely-not-a-command"})
	if !reflect.DeepEqual(got, []string{"definitely-not-a-command"}) {
		t.Errorf("Unresolved = %v", got)
	}
}

func targets(t *testing.T, names ...string) []Target {
	var out []Target
	for _, n := range names {
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, n)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		out = append(out, Target{Name: n, Dir: dir, Env: []string{"WT_REPO=" + n}})
	}
	return out
}

func TestRunParallel(t *testing.T) {
	var out bytes.Buffer
	ts := targets(t, "api", "web", "bad")
	script := `if [ "$WT_REPO" = bad ]; then echo broken >&2; exit 3; fi; echo "$WT_REPO: $1 $2"; pwd`
	results := Run(context.Background(), script, ts, Options{Shell: "/bin/sh", Args: []string{"hello", "world"}, Out: &out})

	if results[0].Err != nil || results[1].Err != nil || results[2].ExitCode != 3 {
		t.Fatalf("results = %+v", results)
	}
	text := out.String()
	for _, want := range []string{"── api ── ok\napi: hello world\n" + ts[0].Dir, "── web ── ok\nweb: hello world", "── bad ── failed (exit 3)\nbroken"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
	if got := Summary(results); got != "api ok · web ok · bad failed (exit 3)" {
		t.Errorf("Summary = %q", got)
	}
}

func TestRunParallelHasNoInput(t *testing.T) {
	var out bytes.Buffer
	results := Run(context.Background(), `read line || echo "no input"; echo "prompt=$GIT_TERMINAL_PROMPT"`, targets(t, "a"),
		Options{Shell: "/bin/sh", Out: &out})
	if results[0].Err != nil || !strings.Contains(out.String(), "no input") || !strings.Contains(out.String(), "prompt=0") {
		t.Errorf("results = %+v, output:\n%s", results, out.String())
	}
}

func TestRunSerialUsesTerminal(t *testing.T) {
	// A real file, like the terminal: each target reads its own line.
	in := filepath.Join(t.TempDir(), "in")
	if err := os.WriteFile(in, []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out bytes.Buffer
	results := Run(context.Background(), `read line; echo "got $line in $WT_REPO"`, targets(t, "a", "b"),
		Options{Shell: "/bin/sh", Serial: true, In: f, Out: &out, Err: &out})
	if results[0].Err != nil || results[1].Err != nil {
		t.Fatalf("results = %+v", results)
	}
	if want := "── a ──\ngot first in a\n── b ──\ngot second in b\n"; out.String() != want {
		t.Errorf("output = %q; want %q", out.String(), want)
	}
}
