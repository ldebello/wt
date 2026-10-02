package cli

import (
	"strings"
	"testing"
)

func TestBundleLifecycle(t *testing.T) {
	env := setupRepos(t, "api", "db", "web")

	r := mustRun(t, env, nil, "bundle", "backend", "--repos", "api,db@v1")
	if !strings.Contains(r.out, "Created bundle backend: api, db@v1") {
		t.Errorf("output:\n%s", r.out)
	}
	mustRun(t, env, nil, "bundle", "frontend", "--repos", "web,db@v2")

	r = mustRun(t, env, nil, "bundle", "list")
	if !strings.Contains(r.out, "backend   api, db@v1") || !strings.Contains(r.out, "frontend  web, db@v2") {
		t.Errorf("list:\n%s", r.out)
	}
	r = mustRun(t, env, nil, "bundle", "backend")
	if strings.TrimSpace(r.out) != "backend: api, db@v1" {
		t.Errorf("show:\n%s", r.out)
	}

	// Interactive update keeps pinned branches of preselected repos.
	fake := &fakeUI{multi: [][]string{{"db", "web"}}}
	r = mustRun(t, env, fake, "bundle", "backend", "--repos")
	if !strings.Contains(r.out, "Updated bundle backend: db@v1, web") {
		t.Errorf("output:\n%s", r.out)
	}
	if opts := fake.offered[0]; !opts[0].Selected || opts[1].Label != "db@v1" || !opts[1].Selected || opts[2].Selected {
		t.Errorf("picker options: %+v", opts)
	}

	mustRun(t, env, nil, "bundle", "remove", "frontend")
	if r = mustRun(t, env, nil, "bundle", "list"); strings.Contains(r.out, "frontend") {
		t.Errorf("frontend not removed:\n%s", r.out)
	}
}

func TestBundleErrors(t *testing.T) {
	env := setupRepos(t, "api")
	for _, args := range [][]string{
		{"bundle", "b", "--repos", "ghost"},
		{"bundle", "b", "--repos", "api,api@x"},
		{"bundle", "missing"},
		{"bundle", "remove", "missing"},
		{"bundle", "a/b", "--repos", "api"},
	} {
		if r := run(t, env, nil, args...); r.e == nil {
			t.Errorf("wt %v: expected error", args)
		}
	}
}

func TestWorkspaceWithBundles(t *testing.T) {
	env := setupRepos(t, "api", "db", "web", "tools")
	mustRun(t, env, nil, "bundle", "backend", "--repos", "api,db@main")
	mustRun(t, env, nil, "bundle", "frontend", "--repos", "web,db@other")

	r := mustRun(t, env, nil, "ws", "W", "--bundles", "backend,frontend", "--repos", "tools,api@main")
	for _, want := range []string{
		"Note: db: keeping branch main from bundle backend, ignoring branch other from bundle frontend",
		"Note: api: using branch main from --repos over the workspace branch from bundle backend",
		"web", "tools",
	} {
		if !strings.Contains(r.out, want) {
			t.Errorf("output missing %q:\n%s", want, r.out)
		}
	}
	r = mustRun(t, env, nil, "ws", "list")
	if !strings.Contains(r.out, "api@main  db@main  tools@W  web@W") {
		t.Errorf("list:\n%s", r.out)
	}

	// Interactive bundle selection.
	fake := &fakeUI{multi: [][]string{{"frontend"}}}
	mustRun(t, env, fake, "ws", "V", "--bundles")
	if fake.asked[0] != "Bundles" {
		t.Errorf("asked %v", fake.asked)
	}

	if r := run(t, env, nil, "ws", "X", "--bundles", "nope"); r.e == nil || !strings.Contains(r.e.Error(), "unknown bundle") {
		t.Errorf("got %v", r.e)
	}
}
