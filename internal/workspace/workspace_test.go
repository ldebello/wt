package workspace

import (
	"context"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ldebello/wt/internal/repo"
	"github.com/ldebello/wt/internal/testutil"
)

func TestParseSpecs(t *testing.T) {
	got, err := ParseSpecs([]string{"a@feature/x,b", " c ", "d:release-2.4", "e@x@y"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Spec{{"a", "feature/x", ""}, {"b", "", ""}, {"c", "", ""}, {"d", "", "release-2.4"}, {"e", "x@y", ""}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	for _, s := range want {
		if back, _ := ParseSpec(s.String()); back != s {
			t.Errorf("round trip %v -> %q -> %v", s, s.String(), back)
		}
	}
	for _, bad := range []string{"@x", "a@", "@", ":x", "a:", "a@b:c", "a:b:c"} {
		if _, err := ParseSpec(bad); err == nil {
			t.Errorf("ParseSpec(%q): expected error", bad)
		}
	}
}

func TestCombine(t *testing.T) {
	bundles := []NamedSpecs{
		{Name: "backend", Specs: []Spec{{"api", "", ""}, {"db", "v1", ""}, {"shared", "main", ""}}},
		{Name: "frontend", Specs: []Spec{{"web", "", ""}, {"db", "v2", ""}, {"shared", "main", ""}}},
	}
	explicit := []Spec{{"api", "feature", ""}, {"web", "", ""}, {"tools", "", ""}}
	got, notes := Combine(bundles, explicit)
	want := []Spec{{"api", "feature", ""}, {"db", "v1", ""}, {"shared", "main", ""}, {"web", "", ""}, {"tools", "", ""}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Combine = %v; want %v", got, want)
	}
	joined := strings.Join(notes, "\n")
	if len(notes) != 2 || !strings.Contains(joined, "db: keeping branch v1 from bundle backend, ignoring branch v2 from bundle frontend") ||
		!strings.Contains(joined, "api: using branch feature from --repos over the workspace branch from bundle backend") {
		t.Errorf("notes:\n%s", joined)
	}

	// An explicit base overrides a bundle's branch, with a note.
	got, notes = Combine([]NamedSpecs{{Name: "b", Specs: []Spec{{"db", "v1", ""}}}}, []Spec{{Repo: "db", Base: "release"}})
	if got[0].Base != "release" || got[0].Branch != "" || len(notes) != 1 || !strings.Contains(notes[0], "a new workspace branch from release") {
		t.Errorf("got %v, notes %v", got, notes)
	}

	// An explicit repo without a branch doesn't override a bundle's branch.
	got, notes = Combine([]NamedSpecs{{Name: "b", Specs: []Spec{{"db", "v1", ""}}}}, []Spec{{"db", "", ""}})
	if got[0].Branch != "v1" || len(notes) != 1 {
		t.Errorf("got %v, notes %v", got, notes)
	}
}

type fixture struct {
	env *testutil.Env
	ix  repo.Index
	m   Manager
	ups map[string]string // upstream path per repo
}

func newFixture(t *testing.T, repos ...string) *fixture {
	t.Helper()
	env := testutil.Setup(t)
	f := &fixture{
		env: env,
		ix:  repo.Index{Dir: filepath.Join(env.Home, ".repos")},
		ups: map[string]string{},
	}
	f.m = Manager{Index: f.ix, Dir: filepath.Join(env.Home, "workspaces")}
	for _, name := range repos {
		f.ups[name] = env.NewUpstream(t, name, "main")
	}
	return f
}

func (f *fixture) clone(t *testing.T, name string) {
	t.Helper()
	if _, err := f.ix.Clone(context.Background(), f.ups[name], name, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) create(t *testing.T, ws string, specs []Spec) []Step {
	t.Helper()
	ctx := context.Background()
	steps, err := f.m.Plan(ctx, ws, specs)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.m.Apply(ctx, ws, steps); err != nil {
		t.Fatal(err)
	}
	return steps
}

func head(t *testing.T, dir string) string {
	return testutil.Git(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
}

func TestCreateResolvesBranches(t *testing.T) {
	f := newFixture(t, "fresh", "remote", "pinned")
	testutil.Commit(t, f.ups["remote"], "DOM-1", "r.txt", "on remote")
	testutil.Commit(t, f.ups["pinned"], "release", "p.txt", "release")
	for _, name := range []string{"fresh", "remote", "pinned"} {
		f.clone(t, name)
	}

	steps := f.create(t, "DOM-1", []Spec{{"fresh", "", ""}, {"remote", "", ""}, {"pinned", "release", ""}})
	actions := []Action{steps[0].Action, steps[1].Action, steps[2].Action}
	if !reflect.DeepEqual(actions, []Action{CreateBranch, TrackRemote, TrackRemote}) {
		t.Errorf("actions = %v", actions)
	}
	if steps[0].Start != "origin/main" {
		t.Errorf("start = %q", steps[0].Start)
	}

	ws := f.m.Path("DOM-1")
	if got := head(t, filepath.Join(ws, "fresh")); got != "DOM-1" {
		t.Errorf("fresh on %s", got)
	}
	// A new branch has no upstream (push.autoSetupRemote handles the first push).
	if out, err := tryGit(filepath.Join(ws, "fresh"), "rev-parse", "--abbrev-ref", "@{u}"); err == nil {
		t.Errorf("new branch unexpectedly tracks %s", out)
	}
	if got := testutil.Git(t, filepath.Join(ws, "remote"), "rev-parse", "--abbrev-ref", "@{u}"); got != "origin/DOM-1" {
		t.Errorf("remote upstream = %s", got)
	}
	if got := head(t, filepath.Join(ws, "pinned")); got != "release" {
		t.Errorf("pinned on %s", got)
	}

	loaded, err := f.m.Load("DOM-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Members) != 3 || loaded.Members[0].Repo != "fresh" || loaded.Members[0].Branch != "DOM-1" {
		t.Errorf("Load = %+v", loaded)
	}
}

func TestCreateDefaultBranchWorksAlongsidePrimary(t *testing.T) {
	f := newFixture(t, "app")
	f.clone(t, "app")
	f.create(t, "W", []Spec{{"app", "main", ""}})
	if got := head(t, filepath.Join(f.m.Path("W"), "app")); got != "main" {
		t.Errorf("on %s", got)
	}
}

func TestCreateIncrementalLeavesExistingUntouched(t *testing.T) {
	f := newFixture(t, "a", "b")
	f.clone(t, "a")
	f.clone(t, "b")
	f.create(t, "W", []Spec{{"a", "", ""}})
	work := filepath.Join(f.m.Path("W"), "a", "work.txt")
	testutil.WriteFile(t, work, "uncommitted")

	steps := f.create(t, "W", []Spec{{"a", "other", ""}, {"b", "", ""}})
	if steps[0].Action != Reuse || !strings.Contains(steps[0].Describe(), "requested branch other") {
		t.Errorf("step = %+v (%s)", steps[0], steps[0].Describe())
	}
	if data, _ := os.ReadFile(work); string(data) != "uncommitted" {
		t.Error("existing worktree was modified")
	}
	if got := head(t, filepath.Join(f.m.Path("W"), "b")); got != "W" {
		t.Errorf("b on %s", got)
	}
}

func TestCreateFromBase(t *testing.T) {
	f := newFixture(t, "app")
	testutil.Commit(t, f.ups["app"], "release", "r.txt", "r")
	f.clone(t, "app")
	ctx := context.Background()

	// app:release creates the workspace branch from origin/release.
	steps := f.create(t, "W", []Spec{{Repo: "app", Base: "release"}})
	if steps[0].Action != CreateBranch || steps[0].Start != "origin/release" || steps[0].Branch != "W" {
		t.Errorf("step = %+v", steps[0])
	}
	if _, err := os.Stat(filepath.Join(f.m.Path("W"), "app", "r.txt")); err != nil {
		t.Error("branch not based on release")
	}

	// Two workspaces can start from the same base: each gets its own branch.
	steps = f.create(t, "V", []Spec{{Repo: "app", Base: "release"}})
	if steps[0].Branch != "V" || steps[0].Start != "origin/release" {
		t.Errorf("step = %+v", steps[0])
	}

	// Remove the worktree but keep the branch: re-adding checks it out and
	// reports the base as ignored.
	testutil.Git(t, f.ix.BarePath("app"), "worktree", "remove", filepath.Join(f.m.Path("W"), "app"))
	steps = f.create(t, "W", []Spec{{Repo: "app", Base: "release"}})
	if steps[0].Action != CheckoutLocal || !strings.Contains(steps[0].Describe(), "base release ignored: it already exists") {
		t.Errorf("step = %+v (%s)", steps[0], steps[0].Describe())
	}

	if _, err := f.m.Plan(ctx, "X", []Spec{{Repo: "app", Base: "nope"}}); err == nil || !strings.Contains(err.Error(), `base branch "nope"`) {
		t.Errorf("expected missing base error, got %v", err)
	}
}

func TestCreateFromLocalBaseWithOwnCommits(t *testing.T) {
	f := newFixture(t, "app")
	f.clone(t, "app")

	// Stack DOM-2 on DOM-1, which has unpushed commits: the local branch is
	// the base, not origin.
	f.create(t, "DOM-1", []Spec{{Repo: "app"}})
	mine := testutil.Commit(t, filepath.Join(f.m.Path("DOM-1"), "app"), "DOM-1", "one.txt", "1")
	steps := f.create(t, "DOM-2", []Spec{{Repo: "app", Base: "DOM-1"}})
	if steps[0].Start != "DOM-1" {
		t.Errorf("start = %q", steps[0].Start)
	}
	if got := testutil.Git(t, filepath.Join(f.m.Path("DOM-2"), "app"), "rev-parse", "HEAD"); got != mine {
		t.Errorf("DOM-2 starts at %s; want %s", got, mine)
	}
}

func TestPlanReportsAllProblems(t *testing.T) {
	f := newFixture(t, "app")
	f.clone(t, "app")
	f.create(t, "A", []Spec{{"app", "shared", ""}})

	_, err := f.m.Plan(context.Background(), "B", []Spec{{"app", "shared", ""}, {"ghost", "", ""}, {"app", "", ""}})
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"already checked out at", `unknown repository "ghost"`, "listed more than once"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%v", want, err)
		}
	}
	if f.m.Exists("B") {
		t.Error("workspace created despite plan errors")
	}
}

func TestApplyRollsBackOnFailure(t *testing.T) {
	f := newFixture(t, "a", "b")
	f.clone(t, "a")
	f.clone(t, "b")
	ctx := context.Background()

	steps, err := f.m.Plan(ctx, "W", []Spec{{"a", "", ""}, {"b", "", ""}})
	if err != nil {
		t.Fatal(err)
	}
	// Something appears at b's target between plan and apply.
	testutil.WriteFile(t, filepath.Join(f.m.Path("W"), "b", "blocker"), "x")
	if err := f.m.Apply(ctx, "W", steps); err == nil {
		t.Fatal("expected apply error")
	}
	if _, err := os.Stat(filepath.Join(f.m.Path("W"), "a")); !os.IsNotExist(err) {
		t.Error("worktree a not rolled back")
	}
	if testutil.Git(t, f.ix.BarePath("a"), "branch", "--list", "W") != "" {
		t.Error("branch W in a not rolled back")
	}
	if _, err := os.Stat(filepath.Join(f.m.Path("W"), "b", "blocker")); err != nil {
		t.Error("rollback removed a file it did not create")
	}
}

func TestLoadAndNames(t *testing.T) {
	f := newFixture(t, "app")
	f.clone(t, "app")
	f.create(t, "W", []Spec{{"app", "", ""}})
	testutil.WriteFile(t, filepath.Join(f.m.Path("W"), "CLAUDE.md"), "x")
	if err := os.MkdirAll(f.m.Path("Empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	names, _ := f.m.Names()
	if !reflect.DeepEqual(names, []string{"Empty", "W"}) {
		t.Errorf("Names = %v", names)
	}
	ws, err := f.m.Load("W")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ws.RepoNames(), []string{"app"}) || !reflect.DeepEqual(ws.Other, []string{"CLAUDE.md"}) {
		t.Errorf("Load = %+v", ws)
	}
	if _, err := f.m.Load("missing"); err == nil {
		t.Error("expected error for missing workspace")
	}
}

func TestRemoveMembers(t *testing.T) {
	f := newFixture(t, "merged", "pushed", "local", "dirty")
	for _, name := range []string{"merged", "pushed", "local", "dirty"} {
		f.clone(t, name)
	}
	f.create(t, "W", []Spec{{"merged", "", ""}, {"pushed", "", ""}, {"local", "", ""}, {"dirty", "", ""}})
	ws := f.m.Path("W")
	ctx := context.Background()

	// merged: squash-merged upstream.
	testutil.Commit(t, filepath.Join(ws, "merged"), "W", "m.txt", "m")
	testutil.Commit(t, f.ups["merged"], "main", "m.txt", "m")
	testutil.Git(t, f.ix.BarePath("merged"), "fetch", "-q", "origin")
	// pushed: on origin, not merged.
	testutil.Commit(t, filepath.Join(ws, "pushed"), "W", "p.txt", "p")
	testutil.Git(t, filepath.Join(ws, "pushed"), "push", "-q", "origin", "W")
	// local: committed but neither pushed nor merged.
	testutil.Commit(t, filepath.Join(ws, "local"), "W", "l.txt", "l")
	// dirty: uncommitted file.
	testutil.WriteFile(t, filepath.Join(ws, "dirty", "d.txt"), "d")

	loaded, _ := f.m.Load("W")
	results := f.m.RemoveMembers(ctx, loaded.Members, true)
	byRepo := map[string]RemoveResult{}
	for _, r := range results {
		byRepo[r.Repo] = r
	}

	if r := byRepo["merged"]; !r.Removed || !r.BranchDeleted {
		t.Errorf("merged: %+v", r)
	}
	// pushed: origin/W is deleted, so the local branch is the only copy left.
	if r := byRepo["pushed"]; !r.Removed || r.BranchDeleted || !r.RemoteDeleted || !strings.Contains(r.BranchKept, "is being deleted") {
		t.Errorf("pushed: %+v", r)
	}
	if testutil.Git(t, f.ups["pushed"], "branch", "--list", "W") != "" {
		t.Error("origin branch not deleted")
	}
	if testutil.Git(t, f.ix.BarePath("pushed"), "branch", "--list", "W") == "" {
		t.Error("branch whose only other copy was deleted on origin was deleted locally")
	}
	if r := byRepo["local"]; !r.Removed || r.BranchDeleted || !strings.Contains(r.BranchKept, "not pushed or merged") {
		t.Errorf("local: %+v", r)
	}
	if testutil.Git(t, f.ix.BarePath("local"), "branch", "--list", "W") == "" {
		t.Error("unmerged branch was deleted")
	}
	if r := byRepo["dirty"]; r.Removed || !strings.Contains(r.Skipped, "uncommitted") {
		t.Errorf("dirty: %+v", r)
	}

	left, err := f.m.RemoveDir("W")
	if err != nil || !reflect.DeepEqual(left, []string{"dirty"}) {
		t.Errorf("RemoveDir = %v, %v", left, err)
	}
}

func tryGit(dir string, args ...string) (string, error) {
	out, err := osexec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	return string(out), err
}

func TestCreateFastForwardsLocalBranchBehindOrigin(t *testing.T) {
	f := newFixture(t, "app")
	f.clone(t, "app")
	ctx := context.Background()
	bare := f.ix.BarePath("app")

	// Work on feat, push it, then drop the worktree but keep the branch.
	f.create(t, "W", []Spec{{"app", "feat", ""}})
	wt := filepath.Join(f.m.Path("W"), "app")
	testutil.Commit(t, wt, "feat", "a.txt", "mine")
	testutil.Git(t, wt, "push", "-q", "-u", "origin", "feat")
	testutil.Git(t, bare, "worktree", "remove", wt)

	// A teammate pushes to feat.
	theirs := testutil.Commit(t, f.ups["app"], "feat", "b.txt", "theirs")
	testutil.Git(t, bare, "fetch", "-q", "origin")

	steps := f.create(t, "W", []Spec{{"app", "feat", ""}})
	if s := steps[0]; s.Action != CheckoutLocal || s.Behind != 1 || s.Ahead != 0 || !strings.Contains(s.Describe(), "updated with 1 new commits from origin/feat") {
		t.Errorf("step = %+v (%s)", s, s.Describe())
	}
	if got := testutil.Git(t, wt, "rev-parse", "HEAD"); got != theirs {
		t.Errorf("HEAD = %s; want %s", got, theirs)
	}

	// Diverged: local commit plus another remote commit -> left alone.
	local := testutil.Commit(t, wt, "feat", "c.txt", "local only")
	testutil.Commit(t, f.ups["app"], "feat", "d.txt", "remote only")
	testutil.Git(t, bare, "fetch", "-q", "origin")
	testutil.Git(t, bare, "worktree", "remove", wt)

	steps, err := f.m.Plan(ctx, "W", []Spec{{"app", "feat", ""}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.m.Apply(ctx, "W", steps); err != nil {
		t.Fatal(err)
	}
	if s := steps[0]; s.FastForward() || !strings.Contains(s.Describe(), "diverged from origin/feat: 1 local and 1 remote commits") {
		t.Errorf("step = %+v (%s)", s, s.Describe())
	}
	if got := testutil.Git(t, wt, "rev-parse", "HEAD"); got != local {
		t.Errorf("diverged branch moved: HEAD = %s", got)
	}
}

func TestRemoveKeepsDetachedCommits(t *testing.T) {
	f := newFixture(t, "app")
	f.clone(t, "app")
	f.create(t, "W", []Spec{{"app", "", ""}})
	wt := filepath.Join(f.m.Path("W"), "app")
	testutil.Git(t, wt, "checkout", "-q", "--detach")
	testutil.WriteFile(t, filepath.Join(wt, "d.txt"), "d")
	testutil.Git(t, wt, "add", "d.txt")
	testutil.Git(t, wt, "commit", "-q", "-m", "detached work")

	loaded, _ := f.m.Load("W")
	r := f.m.RemoveMembers(context.Background(), loaded.Members, false)[0]
	if r.Removed || !strings.Contains(r.Skipped, "not on any branch") {
		t.Errorf("detached with commits: %+v", r)
	}

	// Back on a branch-contained commit, it is removed.
	testutil.Git(t, wt, "checkout", "-q", "--detach", "W")
	loaded, _ = f.m.Load("W")
	if r := f.m.RemoveMembers(context.Background(), loaded.Members, false)[0]; !r.Removed {
		t.Errorf("detached on a branch commit: %+v", r)
	}
}
