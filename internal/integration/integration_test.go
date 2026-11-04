package integration

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldebello/wt/internal/config"
	"github.com/ldebello/wt/internal/testutil"
)

func testWorkspace(t *testing.T) Workspace {
	dir := t.TempDir()
	return Workspace{
		Name: "PROJ-1",
		Path: dir,
		Repos: []Repo{
			{Name: "api", Branch: "PROJ-1", Path: filepath.Join(dir, "api")},
			{Name: "web", Branch: "", Path: filepath.Join(dir, "web")},
		},
	}
}

func TestAllIgnoresUnknownHarnesses(t *testing.T) {
	cfg := config.Default()
	cfg.Integrations.Harness["cursor"] = config.Harness{Enabled: true}
	var names []string
	for _, i := range All(cfg, t.TempDir(), io.Discard) {
		names = append(names, i.Name())
	}
	if got := strings.Join(names, ","); got != "codegraph,folder-customization,harness claude,harness generic" {
		t.Errorf("names = %s", got)
	}
}

func TestHarnessGeneratesAndRespectsEdits(t *testing.T) {
	ctx := context.Background()
	ws := testWorkspace(t)
	h := NewHarness("claude", config.Harness{Enabled: true}, t.TempDir(), true)
	if err := h.Check(); err != nil {
		t.Fatal(err)
	}
	if err := h.OnWorkspaceCreated(ctx, ws); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ws.Path, "CLAUDE.md")
	data, _ := os.ReadFile(path)
	for _, want := range []string{Marker, "# Workspace PROJ-1", "| `api/` | `PROJ-1` |", "| `web/` | `(detached)` |", "CodeGraph index"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("CLAUDE.md missing %q:\n%s", want, data)
		}
	}

	// Regenerated while the marker is present.
	ws.Repos = ws.Repos[:1]
	if err := h.OnWorkspaceCreated(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if data, _ = os.ReadFile(path); strings.Contains(string(data), "web/") {
		t.Error("file not regenerated")
	}

	// Left alone once the user removes the marker; not deleted on removal.
	testutil.WriteFile(t, path, "my notes")
	if err := h.OnWorkspaceCreated(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if err := h.OnWorkspaceRemoved(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if data, _ = os.ReadFile(path); string(data) != "my notes" {
		t.Errorf("edited file touched: %q", data)
	}
}

func TestHarnessRemovesGeneratedFile(t *testing.T) {
	ctx := context.Background()
	ws := testWorkspace(t)
	h := NewHarness("generic", config.Harness{Enabled: true}, t.TempDir(), false)
	if err := h.OnWorkspaceCreated(ctx, ws); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(ws.Path, "AGENTS.md"))
	if strings.Contains(string(data), "CodeGraph") || !strings.Contains(string(data), "`api/` on branch `PROJ-1`") {
		t.Errorf("AGENTS.md:\n%s", data)
	}
	if err := h.OnWorkspaceRemoved(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws.Path, "AGENTS.md")); !os.IsNotExist(err) {
		t.Error("generated file not removed")
	}
}

func TestHarnessUserTemplateAndErrors(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	testutil.WriteFile(t, filepath.Join(home, "templates", "mine.md"), "custom {{.Name}}{{range .Repos}} {{.Name}}{{end}}\n")
	ws := testWorkspace(t)

	h := NewHarness("generic", config.Harness{Enabled: true, Template: "mine"}, home, false)
	if err := h.OnWorkspaceCreated(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(ws.Path, "AGENTS.md")); !strings.Contains(string(data), "custom PROJ-1 api web") {
		t.Errorf("AGENTS.md:\n%s", data)
	}

	if err := NewHarness("cursor", config.Harness{}, home, false).Check(); err == nil || !strings.Contains(err.Error(), "unknown harness") {
		t.Errorf("expected unknown harness error, got %v", err)
	}
	if err := NewHarness("claude", config.Harness{Template: "nope"}, home, false).Check(); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected missing template error, got %v", err)
	}
	testutil.WriteFile(t, filepath.Join(home, "templates", "broken.md"), "{{.Nope")
	if err := NewHarness("claude", config.Harness{Template: "broken"}, home, false).Check(); err == nil {
		t.Error("expected parse error")
	}
}

func TestCodegraph(t *testing.T) {
	testutil.Setup(t)
	ctx := context.Background()
	ws := testWorkspace(t)
	c := &Codegraph{enabled: true, out: io.Discard}

	t.Setenv("PATH", t.TempDir())
	if err := c.Check(); err == nil {
		t.Fatal("expected missing binary error")
	}

	log := filepath.Join(t.TempDir(), "codegraph.log")
	testutil.StubBinary(t, "codegraph", `echo "$@" >> `+log+`; [ "$1" = init ] && /bin/mkdir -p "$3/.codegraph"; exit 0`)
	for i := 0; i < 2; i++ {
		if err := c.OnWorkspaceCreated(ctx, ws); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(log)
	if got := strings.TrimSpace(string(data)); got != "init --yes "+ws.Path+"\nsync "+ws.Path {
		t.Errorf("calls:\n%s", got)
	}
	if err := c.OnWorkspaceRemoved(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws.Path, ".codegraph")); !os.IsNotExist(err) {
		t.Error(".codegraph not removed")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFolderCustomization(t *testing.T) {
	ctx := context.Background()
	ws := testWorkspace(t)
	ws.Repos[1].ReadOnly = true
	f := &FolderCustomization{enabled: true, out: io.Discard}
	file := filepath.Join(ws.Path, ".vscode", "settings.json")

	// Created from nothing, only for the read-only repository.
	if err := f.OnWorkspaceCreated(ctx, ws); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, file)
	for _, want := range []string{`"folder-customization.folders"`, `"path": "web"`, `"color": "foldercustomization.AnsiColor4"`, `"badge": "Read Only"`, `"tooltip": "Read Only Repo"`} {
		if !strings.Contains(got, want) {
			t.Errorf("settings missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"api"`) {
		t.Errorf("writable repo marked:\n%s", got)
	}

	// Idempotent, and other settings and hand-made entries survive, in order.
	user := `{
    "editor.tabSize": 2,
    "folder-customization.folders": [
        {"path": "api", "badge": "Mine"},
        {"path": "gone", "badge": "Read Only", "tooltip": "Read Only Repo"}
    ],
    "files.exclude": {"a": true}
}`
	os.WriteFile(file, []byte(user), 0o644)
	for range 2 {
		if err := f.OnWorkspaceCreated(ctx, ws); err != nil {
			t.Fatal(err)
		}
	}
	got = readFile(t, file)
	if strings.Contains(got, `"gone"`) || !strings.Contains(got, `"Mine"`) || strings.Count(got, `"path": "web"`) != 1 {
		t.Errorf("entries not merged:\n%s", got)
	}
	if strings.Index(got, "editor.tabSize") > strings.Index(got, "folder-customization") || strings.Index(got, "folder-customization") > strings.Index(got, "files.exclude") {
		t.Errorf("key order changed:\n%s", got)
	}

	// A repo that stops being read-only loses its entry.
	ws.Repos[1].ReadOnly = false
	f.OnWorkspaceCreated(ctx, ws)
	if got = readFile(t, file); strings.Contains(got, `"web"`) || !strings.Contains(got, `"Mine"`) {
		t.Errorf("stale entry:\n%s", got)
	}

	// Comments: left alone with an error.
	os.WriteFile(file, []byte("{\n// hi\n}"), 0o644)
	ws.Repos[1].ReadOnly = true
	if err := f.OnWorkspaceCreated(ctx, ws); err == nil {
		t.Error("expected an error for a file with comments")
	}
	if readFile(t, file) != "{\n// hi\n}" {
		t.Error("file with comments was modified")
	}
}

func TestFolderCustomizationNoReadOnlyWritesNothing(t *testing.T) {
	ws := testWorkspace(t)
	f := &FolderCustomization{enabled: true, out: io.Discard}
	if err := f.OnWorkspaceCreated(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws.Path, ".vscode")); err == nil {
		t.Error(".vscode created without read-only repos")
	}
}

func TestFolderCustomizationRemovedCleansUp(t *testing.T) {
	ctx := context.Background()
	ws := testWorkspace(t)
	ws.Repos[1].ReadOnly = true
	f := &FolderCustomization{enabled: true, out: io.Discard}
	f.OnWorkspaceCreated(ctx, ws)
	if err := f.OnWorkspaceRemoved(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws.Path, ".vscode")); err == nil {
		t.Error(".vscode left behind")
	}

	// With the user's own settings, only wt's entries go.
	file := filepath.Join(ws.Path, ".vscode", "settings.json")
	os.MkdirAll(filepath.Dir(file), 0o755)
	os.WriteFile(file, []byte(`{"editor.tabSize": 2}`), 0o644)
	f.OnWorkspaceCreated(ctx, ws)
	f.OnWorkspaceRemoved(ctx, ws)
	if got := readFile(t, file); strings.Contains(got, "folder-customization") || !strings.Contains(got, "editor.tabSize") {
		t.Errorf("settings after removal:\n%s", got)
	}
}
