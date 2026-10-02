package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/ldebello/wt/internal/testutil"
	"github.com/ldebello/wt/internal/ui"
)

// fakeUI returns canned answers in order and records what was asked.
type fakeUI struct {
	multi    [][]string
	selects  []string
	confirms []bool
	asked    []string
	offered  [][]ui.Option
}

func (f *fakeUI) MultiSelect(title string, options []ui.Option) ([]string, error) {
	f.asked = append(f.asked, title)
	f.offered = append(f.offered, options)
	if len(f.multi) == 0 {
		return nil, ui.ErrNoTTY
	}
	answer := f.multi[0]
	f.multi = f.multi[1:]
	return answer, nil
}

func (f *fakeUI) Select(title string, options []ui.Option) (string, error) {
	f.asked = append(f.asked, title)
	f.offered = append(f.offered, options)
	if len(f.selects) == 0 {
		return "", ui.ErrNoTTY
	}
	answer := f.selects[0]
	f.selects = f.selects[1:]
	return answer, nil
}

func (f *fakeUI) Confirm(title string, def bool) (bool, error) {
	f.asked = append(f.asked, title)
	if len(f.confirms) == 0 {
		return false, ui.ErrNoTTY
	}
	answer := f.confirms[0]
	f.confirms = f.confirms[1:]
	return answer, nil
}

type result struct {
	out, err string
	e        error
}

// run executes wt in-process with a fresh App and command tree.
func run(t *testing.T, env *testutil.Env, prompter ui.Prompter, args ...string) result {
	t.Helper()
	if prompter == nil {
		prompter = &fakeUI{}
	}
	var out, errOut bytes.Buffer
	app := &App{Out: &out, Err: &errOut, UI: prompter, Home: env.WTHome}
	root := NewRootCmd(app, "test")
	root.SetArgs(PrepareArgs(args))
	e := root.ExecuteContext(context.Background())
	return result{out: out.String(), err: errOut.String(), e: e}
}

// mustRun is run that fails the test on error.
func mustRun(t *testing.T, env *testutil.Env, prompter ui.Prompter, args ...string) result {
	t.Helper()
	r := run(t, env, prompter, args...)
	if r.e != nil {
		t.Fatalf("wt %v: %v\nstdout:\n%s\nstderr:\n%s", args, r.e, r.out, r.err)
	}
	return r
}
