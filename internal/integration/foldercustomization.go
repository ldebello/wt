package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	folderCustomizationKey = "folder-customization.folders"
	// ExtensionID and ExtensionURL identify the VS Code extension that reads
	// folder-customization.folders.
	ExtensionID  = "j3lte.folder-customization"
	ExtensionURL = "https://marketplace.visualstudio.com/items?itemName=" + ExtensionID

	roColor   = "foldercustomization.AnsiColor4"
	roBadge   = "Read Only"
	roTooltip = "Read Only Repo"
)

// FolderCustomizationHint tells how to install the VS Code extension.
const FolderCustomizationHint = "install the Folder Customization extension in VS Code: " + ExtensionURL + "\n" +
	"         or: code --install-extension " + ExtensionID

// FolderCustomization marks the read-only repositories of a workspace in
// VS Code's explorer, through the Folder Customization extension, by writing
// folder-customization.folders in <workspace>/.vscode/settings.json.
type FolderCustomization struct {
	enabled bool
	out     io.Writer
}

func (f *FolderCustomization) Name() string  { return "folder-customization" }
func (f *FolderCustomization) Enabled() bool { return f.enabled }

// Check has nothing to verify: VS Code keeps its extensions out of reach.
func (f *FolderCustomization) Check() error { return nil }

// Setup reminds that the extension itself must be installed.
func (f *FolderCustomization) Setup(ctx context.Context) error {
	fmt.Fprintf(f.out, "Folder Customization needs its VS Code extension; %s\n", FolderCustomizationHint)
	return nil
}

type folderEntry struct {
	Path    string `json:"path"`
	Color   string `json:"color,omitempty"`
	Badge   string `json:"badge,omitempty"`
	Tooltip string `json:"tooltip,omitempty"`
}

// ours reports whether wt wrote this entry, so it can be replaced.
func (e folderEntry) ours() bool { return e.Badge == roBadge && e.Tooltip == roTooltip }

// OnWorkspaceCreated rewrites the entries for the read-only repositories and
// leaves every other setting and entry as it was.
func (f *FolderCustomization) OnWorkspaceCreated(ctx context.Context, ws Workspace) error {
	var want []folderEntry
	for _, r := range ws.Repos {
		if !r.ReadOnly {
			continue
		}
		rel, err := filepath.Rel(ws.Path, r.Path)
		if err != nil || rel == "" {
			rel = r.Name
		}
		want = append(want, folderEntry{Path: filepath.ToSlash(rel), Color: roColor, Badge: roBadge, Tooltip: roTooltip})
	}
	return syncFolderSettings(ws.Path, want)
}

// OnWorkspaceRemoved drops wt's entries, and the settings file when nothing
// else is left in it.
func (f *FolderCustomization) OnWorkspaceRemoved(ctx context.Context, ws Workspace) error {
	return syncFolderSettings(ws.Path, nil)
}

func syncFolderSettings(wsPath string, want []folderEntry) error {
	file := filepath.Join(wsPath, ".vscode", "settings.json")
	data, err := os.ReadFile(file)
	existed := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s, err := parseSettings(data)
	if err != nil {
		return fmt.Errorf("%s is not plain JSON (comments or trailing commas?); wt leaves it alone: %w", file, err)
	}

	var current []json.RawMessage
	if raw, ok := s.vals[folderCustomizationKey]; ok {
		if err := json.Unmarshal(raw, &current); err != nil {
			return fmt.Errorf("%s: %s is not a list; wt leaves it alone", file, folderCustomizationKey)
		}
	}
	taken := map[string]bool{}
	var entries []json.RawMessage
	for _, raw := range current {
		var e folderEntry
		if json.Unmarshal(raw, &e) == nil && e.ours() {
			continue
		}
		entries = append(entries, raw)
		taken[e.Path] = true
	}
	for _, e := range want {
		if taken[e.Path] {
			continue // customised by hand
		}
		raw, _ := json.Marshal(e)
		entries = append(entries, raw)
	}

	if len(entries) > 0 {
		raw, _ := json.Marshal(entries)
		s.set(folderCustomizationKey, raw)
	} else {
		s.delete(folderCustomizationKey)
	}

	if len(s.keys) == 0 {
		if !existed {
			return nil
		}
		if err := os.Remove(file); err != nil {
			return err
		}
		_ = os.Remove(filepath.Dir(file)) // only when empty
		return nil
	}
	out, err := s.marshal()
	if err != nil {
		return err
	}
	if existed && bytes.Equal(out, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, out, 0o644)
}

// settings is a JSON object that keeps its keys in file order, so rewriting
// it does not shuffle what the user wrote.
type settings struct {
	keys []string
	vals map[string]json.RawMessage
}

func parseSettings(data []byte) (*settings, error) {
	s := &settings{vals: map[string]json.RawMessage{}}
	if len(bytes.TrimSpace(data)) == 0 {
		return s, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("expected a JSON object")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		s.set(key, raw)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("unexpected content after the JSON object")
	}
	return s, nil
}

func (s *settings) set(key string, raw json.RawMessage) {
	if _, ok := s.vals[key]; !ok {
		s.keys = append(s.keys, key)
	}
	s.vals[key] = raw
}

func (s *settings) delete(key string) {
	if _, ok := s.vals[key]; !ok {
		return
	}
	delete(s.vals, key)
	for i, k := range s.keys {
		if k == key {
			s.keys = append(s.keys[:i], s.keys[i+1:]...)
			break
		}
	}
}

func (s *settings) marshal() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, k := range s.keys {
		name, _ := json.Marshal(k)
		var val bytes.Buffer
		if err := json.Indent(&val, s.vals[k], "    ", "    "); err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "    %s: %s", name, val.Bytes())
		if i < len(s.keys)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.Bytes(), nil
}
