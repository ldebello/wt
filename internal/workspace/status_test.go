package workspace

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ldebello/wt/internal/testutil"
)

func TestStatusSummary(t *testing.T) {
	f := newFixture(t, "a", "b")
	f.clone(t, "a")
	f.clone(t, "b")
	ctx := context.Background()

	summary := func(name string) (string, bool) {
		t.Helper()
		ws, err := f.m.Load(name)
		if err != nil {
			t.Fatal(err)
		}
		return f.m.Status(ctx, ws).Summary()
	}
	fetch := func() {
		for _, r := range []string{"a", "b"} {
			testutil.Git(t, f.ix.BarePath(r), "fetch", "-q", "origin")
		}
	}

	f.create(t, "Fresh", []Spec{{"a", ""}, {"b", ""}}, Options{})
	if label, safe := summary("Fresh"); label != "no commits yet" || safe {
		t.Errorf("Fresh: %q %v", label, safe)
	}

	// One repo squash-merged upstream, the other untouched: safe.
	f.create(t, "Done", []Spec{{"a", ""}, {"b", ""}}, Options{})
	testutil.Commit(t, filepath.Join(f.m.Path("Done"), "a"), "Done", "x.txt", "x")
	testutil.Commit(t, f.ups["a"], "main", "x.txt", "x")
	fetch()
	if label, safe := summary("Done"); label != "merged (safe to remove)" || !safe {
		t.Errorf("Done: %q %v", label, safe)
	}

	// Pushed, then merged with a merge commit upstream: no commits ahead of
	// origin/main any more, but still merged rather than untouched.
	f.create(t, "MergeCommit", []Spec{{"b", ""}, {"a", "main"}}, Options{})
	testutil.Commit(t, filepath.Join(f.m.Path("MergeCommit"), "b"), "MergeCommit", "w.txt", "w")
	testutil.Git(t, filepath.Join(f.m.Path("MergeCommit"), "b"), "push", "-q", "-u", "origin", "MergeCommit")
	testutil.Git(t, f.ups["b"], "merge", "-q", "--no-ff", "-m", "merge", "MergeCommit")
	testutil.Git(t, f.ups["b"], "branch", "-q", "-D", "MergeCommit")
	fetch()
	if label, safe := summary("MergeCommit"); label != "merged (safe to remove)" || !safe {
		t.Errorf("MergeCommit: %q %v", label, safe)
	}
	// A local commit on the default branch is work of its own.
	testutil.Commit(t, filepath.Join(f.m.Path("MergeCommit"), "a"), "main", "v.txt", "v")
	if label, safe := summary("MergeCommit"); label != "contains unpushed commits" || safe {
		t.Errorf("MergeCommit with a commit on main: %q %v", label, safe)
	}

	f.create(t, "Local", []Spec{{"a", ""}}, Options{})
	testutil.Commit(t, filepath.Join(f.m.Path("Local"), "a"), "Local", "y.txt", "y")
	if label, safe := summary("Local"); label != "contains unpushed commits" || safe {
		t.Errorf("Local: %q %v", label, safe)
	}

	testutil.Git(t, filepath.Join(f.m.Path("Local"), "a"), "push", "-q", "origin", "Local")
	if label, _ := summary("Local"); label != "pushed, not merged yet" {
		t.Errorf("Local pushed: %q", label)
	}

	testutil.WriteFile(t, filepath.Join(f.m.Path("Local"), "a", "z.txt"), "z")
	if label, _ := summary("Local"); label != "uncommitted changes in a" {
		t.Errorf("Local dirty: %q", label)
	}

	f.create(t, "Empty", nil, Options{})
	if label, safe := summary("Empty"); label != "empty" || safe {
		t.Errorf("Empty: %q %v", label, safe)
	}
}
