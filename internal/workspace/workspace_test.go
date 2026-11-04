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

	"github.com/ldebello/wt/internal/git"
	"github.com/ldebello/wt/internal/repo"
	"github.com/ldebello/wt/internal/testutil"
)

func TestParseSpecs(t *testing.T) {
	got, err := ParseSpecs([]string{"a@feature/x,b", " c ", "d:release-2.4", "e@x@y"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Spec{{"a", "feature/x", "", false}, {"b", "", "", false}, {"c", "", "", false}, {"d", "", "release-2.4", false}, {"e", "x@y", "", false}}
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

func TestParseSpecsReadOnly(t *testing.T) {
	for in, want := range map[string]Spec{
		"a#ro":         {Repo: "a", ReadOnly: true},
		"a@release#ro": {Repo: "a", Branch: "release", ReadOnly: true},
	} {
		got, err := ParseSpec(in)
		if err != nil || got != want {
			t.Errorf("ParseSpec(%q) = %+v, %v; want %+v", in, got, err, want)
		}
		if back := got.String(); back != in {
			t.Errorf("round trip %q -> %+v -> %q", in, got, back)
		}
	}
	// A base is for a branch of your own to write on: it cannot be read-only.
	if _, err := ParseSpec("a:release#ro"); err == nil {
		t.Error("ParseSpec(\"a:release#ro\"): expected error")
	}
}

func TestCombine(t *testing.T) {
	bundles := []NamedSpecs{
		{Name: "backend", Specs: []Spec{{"api", "", "", false}, {"db", "v1", "", false}, {"shared", "main", "", false}}},
		{Name: "frontend", Specs: []Spec{{"web", "", "", false}, {"db", "v2", "", false}, {"shared", "main", "", false}}},
	}
	explicit := []Spec{{"api", "feature", "", false}, {"web", "", "", false}, {"tools", "", "", false}}
	got, notes := Combine(bundles, explicit)
	want := []Spec{{"api", "feature", "", false}, {"db", "v1", "", false}, {"shared", "main", "", false}, {"web", "", "", false}, {"tools", "", "", false}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Combine = %v; want %v", got, want)
	}
	joined := strings.Join(notes, "\n")
	if len(notes) != 2 || !strings.Contains(joined, "db: keeping branch v1 from bundle backend, ignoring branch v2 from bundle frontend") ||
		!strings.Contains(joined, "api: using branch feature from --repos over the workspace branch from bundle backend") {
		t.Errorf("notes:\n%s", joined)
	}

	// An explicit base overrides a bundle's branch, with a note.
	got, notes = Combine([]NamedSpecs{{Name: "b", Specs: []Spec{{"db", "v1", "", false}}}}, []Spec{{Repo: "db", Base: "release"}})
	if got[0].Base != "release" || got[0].Branch != "" || len(notes) != 1 || !strings.Contains(notes[0], "a new workspace branch from release") {
		t.Errorf("got %v, notes %v", got, notes)
	}

	// An explicit repo without a branch doesn't override a bundle's branch.
	got, notes = Combine([]NamedSpecs{{Name: "b", Specs: []Spec{{"db", "v1", "", false}}}}, []Spec{{"db", "", "", false}})
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
	testutil.Commit(t, f.ups["remote"], "PROJ-1", "r.txt", "on remote")
	testutil.Commit(t, f.ups["pinned"], "release", "p.txt", "release")
	for _, name := range []string{"fresh", "remote", "pinned"} {
		f.clone(t, name)
	}

	steps := f.create(t, "PROJ-1", []Spec{{"fresh", "", "", false}, {"remote", "", "", false}, {"pinned", "release", "", false}})
	actions := []Action{steps[0].Action, steps[1].Action, steps[2].Action}
	if !reflect.DeepEqual(actions, []Action{CreateBranch, TrackRemote, TrackRemote}) {
		t.Errorf("actions = %v", actions)
	}
	if steps[0].Start != "origin/main" {
		t.Errorf("start = %q", steps[0].Start)
	}

	ws := f.m.Path("PROJ-1")
	if got := head(t, filepath.Join(ws, "fresh")); got != "PROJ-1" {
		t.Errorf("fresh on %s", got)
	}
	// A new branch has no upstream (push.autoSetupRemote handles the first push).
	if out, err := tryGit(filepath.Join(ws, "fresh"), "rev-parse", "--abbrev-ref", "@{u}"); err == nil {
		t.Errorf("new branch unexpectedly tracks %s", out)
	}
	if got := testutil.Git(t, filepath.Join(ws, "remote"), "rev-parse", "--abbrev-ref", "@{u}"); got != "origin/PROJ-1" {
		t.Errorf("remote upstream = %s", got)
	}
	if got := head(t, filepath.Join(ws, "pinned")); got != "release" {
		t.Errorf("pinned on %s", got)
	}

	loaded, err := f.m.Load("PROJ-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Members) != 3 || loaded.Members[0].Repo != "fresh" || loaded.Members[0].Branch != "PROJ-1" {
		t.Errorf("Load = %+v", loaded)
	}
}

func TestCreateDefaultBranchWorksAlongsidePrimary(t *testing.T) {
	f := newFixture(t, "app")
	f.clone(t, "app")
	f.create(t, "W", []Spec{{"app", "main", "", false}})
	if got := head(t, filepath.Join(f.m.Path("W"), "app")); got != "main" {
		t.Errorf("on %s", got)
	}
}

func TestCreateIncrementalLeavesExistingUntouched(t *testing.T) {
	f := newFixture(t, "a", "b")
	f.clone(t, "a")
	f.clone(t, "b")
	f.create(t, "W", []Spec{{"a", "", "", false}})
	work := filepath.Join(f.m.Path("W"), "a", "work.txt")
	testutil.WriteFile(t, work, "uncommitted")

	steps := f.create(t, "W", []Spec{{"a", "other", "", false}, {"b", "", "", false}})
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

	// Stack PROJ-2 on PROJ-1, which has unpushed commits: the local branch is
	// the base, not origin.
	f.create(t, "PROJ-1", []Spec{{Repo: "app"}})
	mine := testutil.Commit(t, filepath.Join(f.m.Path("PROJ-1"), "app"), "PROJ-1", "one.txt", "1")
	steps := f.create(t, "PROJ-2", []Spec{{Repo: "app", Base: "PROJ-1"}})
	if steps[0].Start != "PROJ-1" {
		t.Errorf("start = %q", steps[0].Start)
	}
	if got := testutil.Git(t, filepath.Join(f.m.Path("PROJ-2"), "app"), "rev-parse", "HEAD"); got != mine {
		t.Errorf("PROJ-2 starts at %s; want %s", got, mine)
	}
}

// TestReadOnlyMember covers the whole repo#ro lifecycle: create (detached,
// no branch of its own, a marker in the worktree's git admin dir but nothing
// in the working directory), Load reading it back, Status, and removal.
func TestReadOnlyMember(t *testing.T) {
	f := newFixture(t, "docs")
	testutil.Commit(t, f.ups["docs"], "release", "r.txt", "release only")
	f.clone(t, "docs")
	ctx := context.Background()

	steps := f.create(t, "W", []Spec{{Repo: "docs", ReadOnly: true}})
	if steps[0].Action != CheckoutReadOnly || !steps[0].ReadOnly {
		t.Fatalf("step = %+v", steps[0])
	}
	if got := steps[0].Describe(); got != "read-only at the default branch" {
		t.Errorf("Describe = %q", got)
	}
	path := filepath.Join(f.m.Path("W"), "docs")
	if got := head(t, path); got != "HEAD" {
		t.Errorf("not detached: on %s", got)
	}
	// The marker lives in the worktree's private git admin dir, never in the
	// working directory: it must not show up as an untracked file.
	if dirty, err := git.IsDirty(ctx, path); err != nil || dirty {
		t.Errorf("dirty = %v, %v (marker leaked into the working directory)", dirty, err)
	}

	// Load reads it back the same way.
	ws, err := f.m.Load("W")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := ws.Member("docs")
	if !ok || !m.ReadOnly || m.ReadOnlyRef != "" || m.Branch != "" {
		t.Errorf("member = %+v", m)
	}

	// Status: no branch or commits of its own to judge.
	st := f.m.Status(ctx, ws)
	if ms := st.Members[0]; !ms.ReadOnly || ms.Describe() != "read-only at the default branch" {
		t.Errorf("status = %+v (%s)", ms, ms.Describe())
	}
	if label, safe := st.Summary(); safe || label == "merged (safe to remove)" {
		t.Errorf("Summary = %q, %v", label, safe)
	}

	// Re-adding it is a no-op; asking for it as a regular repo (or a
	// different ref) is rejected or noted instead of silently changing it.
	steps = f.create(t, "W", []Spec{{Repo: "docs", ReadOnly: true}})
	if steps[0].Action != Reuse || steps[0].Note != "" {
		t.Errorf("reuse step = %+v", steps[0])
	}
	if _, err := f.m.Plan(ctx, "W", []Spec{{Repo: "docs"}}); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected error switching to writable, got %v", err)
	}
	steps, err = f.m.Plan(ctx, "W", []Spec{{Repo: "docs", Branch: "release", ReadOnly: true}})
	if err != nil || steps[0].Action != Reuse || !strings.Contains(steps[0].Note, "requested branch release") {
		t.Errorf("reuse with different ref: steps=%+v err=%v", steps, err)
	}

	// Removal: no branch to merge-check or delete, just the worktree.
	r := f.m.RemoveMembers(ctx, ws.Members, true, true)[0]
	if !r.Removed || r.Describe() != "worktree removed (read-only)" {
		t.Errorf("remove = %+v (%s)", r, r.Describe())
	}
}

// TestReadOnlyMemberAtBranch covers repo@branch#ro: pinned to a specific
// branch instead of the repository's default.
func TestReadOnlyMemberAtBranch(t *testing.T) {
	f := newFixture(t, "docs")
	testutil.Commit(t, f.ups["docs"], "release", "r.txt", "release only")
	f.clone(t, "docs")

	steps := f.create(t, "W", []Spec{{Repo: "docs", Branch: "release", ReadOnly: true}})
	if steps[0].Action != CheckoutReadOnly || steps[0].Branch != "release" {
		t.Fatalf("step = %+v", steps[0])
	}
	path := filepath.Join(f.m.Path("W"), "docs")
	if got := testutil.Git(t, path, "rev-parse", "HEAD"); got != testutil.Git(t, f.ups["docs"], "rev-parse", "release") {
		t.Error("not checked out at release")
	}
	ws, _ := f.m.Load("W")
	if m, _ := ws.Member("docs"); m.ReadOnlyRef != "release" {
		t.Errorf("ReadOnlyRef = %q", m.ReadOnlyRef)
	}

	if _, err := f.m.Plan(context.Background(), "X", []Spec{{Repo: "docs", Branch: "no-such-branch", ReadOnly: true}}); err == nil {
		t.Error("expected error for a branch not on origin")
	}
}

func TestPlanReportsAllProblems(t *testing.T) {
	f := newFixture(t, "app")
	f.clone(t, "app")
	f.create(t, "A", []Spec{{"app", "shared", "", false}})

	_, err := f.m.Plan(context.Background(), "B", []Spec{{"app", "shared", "", false}, {"ghost", "", "", false}, {"app", "", "", false}})
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

	steps, err := f.m.Plan(ctx, "W", []Spec{{"a", "", "", false}, {"b", "", "", false}})
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
	f.create(t, "W", []Spec{{"app", "", "", false}})
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
	f.create(t, "W", []Spec{{"merged", "", "", false}, {"pushed", "", "", false}, {"local", "", "", false}, {"dirty", "", "", false}})
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
	results := f.m.RemoveMembers(ctx, loaded.Members, true, false)
	byRepo := map[string]RemoveResult{}
	for _, r := range results {
		byRepo[r.Repo] = r
	}

	if r := byRepo["merged"]; !r.Removed || !r.BranchDeleted {
		t.Errorf("merged: %+v", r)
	}
	// pushed but not merged: origin/W is an open pull request, so it is kept
	// even with deleteRemote, and the local branch can go since origin has it.
	if r := byRepo["pushed"]; !r.Removed || !r.BranchDeleted || r.RemoteDeleted || r.RemoteError == nil || !strings.Contains(r.RemoteError.Error(), "not merged") {
		t.Errorf("pushed: %+v", r)
	}
	if testutil.Git(t, f.ups["pushed"], "branch", "--list", "W") == "" {
		t.Error("unmerged origin branch was deleted")
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

// TestRemoveMembersDeleteUnmerged covers the default local-branch deletion:
// once the worktree is gone, a branch not merged or pushed anywhere is
// deleted anyway, flagged as forced. Origin is unaffected by this.
func TestRemoveMembersDeleteUnmerged(t *testing.T) {
	f := newFixture(t, "local")
	f.clone(t, "local")
	f.create(t, "W", []Spec{{"local", "", "", false}})
	testutil.Commit(t, filepath.Join(f.m.Path("W"), "local"), "W", "l.txt", "l")

	loaded, _ := f.m.Load("W")
	r := f.m.RemoveMembers(context.Background(), loaded.Members, false, true)[0]
	if !r.Removed || !r.BranchDeleted || !r.BranchForced {
		t.Errorf("result = %+v", r)
	}
	if testutil.Git(t, f.ix.BarePath("local"), "branch", "--list", "W") != "" {
		t.Error("unmerged branch was not deleted")
	}
}

// TestCheckBranches covers the removal preview: it must agree with what
// RemoveMembers above actually does, without touching anything.
func TestCheckBranches(t *testing.T) {
	f := newFixture(t, "merged", "local")
	for _, name := range []string{"merged", "local"} {
		f.clone(t, name)
	}
	f.create(t, "W", []Spec{{"merged", "", "", false}, {"local", "", "", false}})
	ws := f.m.Path("W")

	testutil.Commit(t, filepath.Join(ws, "merged"), "W", "m.txt", "m")
	testutil.Commit(t, f.ups["merged"], "main", "m.txt", "m")
	testutil.Git(t, f.ix.BarePath("merged"), "fetch", "-q", "origin")
	testutil.Commit(t, filepath.Join(ws, "local"), "W", "l.txt", "l")

	loaded, _ := f.m.Load("W")
	byRepo := map[string]BranchStatus{}
	for _, s := range f.m.CheckBranches(context.Background(), loaded.Members) {
		byRepo[s.Repo] = s
	}
	if s := byRepo["merged"]; !s.Safe || s.Reason != "" {
		t.Errorf("merged: %+v", s)
	}
	if s := byRepo["local"]; s.Safe || !strings.Contains(s.Reason, "not pushed or merged") {
		t.Errorf("local: %+v", s)
	}

	// Nothing was touched: the worktrees and branches are all still there.
	if _, err := os.Stat(filepath.Join(ws, "local")); err != nil {
		t.Errorf("CheckBranches removed the worktree: %v", err)
	}
	if testutil.Git(t, f.ix.BarePath("local"), "branch", "--list", "W") == "" {
		t.Error("CheckBranches deleted the branch")
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
	f.create(t, "W", []Spec{{"app", "feat", "", false}})
	wt := filepath.Join(f.m.Path("W"), "app")
	testutil.Commit(t, wt, "feat", "a.txt", "mine")
	testutil.Git(t, wt, "push", "-q", "-u", "origin", "feat")
	testutil.Git(t, bare, "worktree", "remove", wt)

	// A teammate pushes to feat.
	theirs := testutil.Commit(t, f.ups["app"], "feat", "b.txt", "theirs")
	testutil.Git(t, bare, "fetch", "-q", "origin")

	steps := f.create(t, "W", []Spec{{"app", "feat", "", false}})
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

	steps, err := f.m.Plan(ctx, "W", []Spec{{"app", "feat", "", false}})
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
	f.create(t, "W", []Spec{{"app", "", "", false}})
	wt := filepath.Join(f.m.Path("W"), "app")
	testutil.Git(t, wt, "checkout", "-q", "--detach")
	testutil.WriteFile(t, filepath.Join(wt, "d.txt"), "d")
	testutil.Git(t, wt, "add", "d.txt")
	testutil.Git(t, wt, "commit", "-q", "-m", "detached work")

	loaded, _ := f.m.Load("W")
	r := f.m.RemoveMembers(context.Background(), loaded.Members, false, false)[0]
	if r.Removed || !strings.Contains(r.Skipped, "not on any branch") {
		t.Errorf("detached with commits: %+v", r)
	}

	// Back on a branch-contained commit, it is removed.
	testutil.Git(t, wt, "checkout", "-q", "--detach", "W")
	loaded, _ = f.m.Load("W")
	if r := f.m.RemoveMembers(context.Background(), loaded.Members, false, false)[0]; !r.Removed {
		t.Errorf("detached on a branch commit: %+v", r)
	}
}

func TestRemoveDoesNotTrustStaleOriginRefs(t *testing.T) {
	f := newFixture(t, "app")
	f.clone(t, "app")
	f.create(t, "W", []Spec{{Repo: "app"}})
	wt := filepath.Join(f.m.Path("W"), "app")
	testutil.Commit(t, wt, "W", "a.txt", "work")
	testutil.Git(t, wt, "push", "-q", "-u", "origin", "W")
	// The pull request is closed and the branch deleted on origin, but the
	// local origin/W is still there.
	testutil.Git(t, f.ups["app"], "branch", "-q", "-D", "W")

	loaded, _ := f.m.Load("W")
	r := f.m.RemoveMembers(context.Background(), loaded.Members, false, false)[0]
	if !r.Removed || r.BranchDeleted || !strings.Contains(r.BranchKept, "not pushed or merged") {
		t.Errorf("result = %+v", r)
	}
	if testutil.Git(t, f.ix.BarePath("app"), "branch", "--list", "W") == "" {
		t.Error("branch whose origin copy is gone was deleted")
	}
}

func TestBaseBranchesAreJudgedAgainstTheirBase(t *testing.T) {
	f := newFixture(t, "app")
	testutil.Commit(t, f.ups["app"], "release", "r.txt", "release only")
	f.clone(t, "app")
	ctx := context.Background()

	f.create(t, "HOTFIX", []Spec{{Repo: "app", Base: "release"}})
	if got := testutil.Git(t, f.ix.BarePath("app"), "config", "--get", "branch.HOTFIX.wtBase"); got != "release" {
		t.Errorf("recorded base = %q", got)
	}
	summary := func() string {
		ws, _ := f.m.Load("HOTFIX")
		label, _ := f.m.Status(ctx, ws).Summary()
		return label
	}
	// Fresh from release: nothing of its own, even though release has commits
	// that main lacks.
	if got := summary(); got != "no commits yet" {
		t.Errorf("fresh: %q", got)
	}

	// Fix pushed and squash-merged into release (not main): merged.
	wt := filepath.Join(f.m.Path("HOTFIX"), "app")
	testutil.Commit(t, wt, "HOTFIX", "fix.txt", "fix")
	testutil.Git(t, wt, "push", "-q", "-u", "origin", "HOTFIX")
	testutil.Commit(t, f.ups["app"], "release", "fix.txt", "fix")
	testutil.Git(t, f.ix.BarePath("app"), "fetch", "-q", "origin")
	if got := summary(); got != "merged (safe to remove)" {
		t.Errorf("merged into release: %q", got)
	}

	// Removal deletes the local branch and, being merged, origin/HOTFIX too.
	ws, _ := f.m.Load("HOTFIX")
	r := f.m.RemoveMembers(ctx, ws.Members, true, false)[0]
	if !r.BranchDeleted || !r.RemoteDeleted {
		t.Errorf("remove = %+v", r)
	}
}
