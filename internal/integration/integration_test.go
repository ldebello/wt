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
	if got := strings.Join(names, ","); got != "codegraph,harness claude,harness generic" {
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
