package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

const hl = "\x1b[1;4;38;5;220m"

func TestHighlightPlain(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	view := "┃ alpha: choose branch\n┃ /rele\n┃ > release-6.3\n┃   prerelease\n↑ up • ↓ down • esc set filter • enter submit"
	got := highlight(view, "rele", []string{"release-6.3", "prerelease"})
	want := "┃ alpha: choose branch\n┃ /" + hl + "rele\x1b[0m\n┃ > " + hl + "rele\x1b[0mase-6.3\n┃   pre" + hl + "rele\x1b[0mase\n↑ up • ↓ down • esc set filter • enter submit"
	if got != want {
		t.Errorf("highlight =\n%q\nwant\n%q", got, want)
	}
}

func TestHighlightStyledAndCaseInsensitive(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	// A styled option: the style is restored after the match.
	view := "\x1b[32m> Release-6.3\x1b[0m"
	got := highlight(view, "rel", []string{"Release-6.3"})
	want := "\x1b[32m> " + hl + "Rel\x1b[0m\x1b[32mease-6.3\x1b[0m"
	if got != want {
		t.Errorf("highlight =\n%q\nwant\n%q", got, want)
	}

	// Nothing typed, or text outside the labels: untouched.
	if got := highlight(view, "", []string{"Release-6.3"}); got != view {
		t.Errorf("empty query changed the view: %q", got)
	}
	help := "↑ up • enter submit"
	if got := highlight(help, "e", []string{"release"}); got != help {
		t.Errorf("help changed: %q", got)
	}
}

func TestHighlightNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if got := highlight("main", "ai", []string{"main"}); got != "m\x1b[1;4mai\x1b[0mn" {
		t.Errorf("highlight = %q", got)
	}
}

func keys(s string) []tea.Msg {
	var msgs []tea.Msg
	for _, r := range s {
		msgs = append(msgs, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return msgs
}

// TestFilterQueryReadsHuh guards the reflection on huh's internals.
func TestFilterQueryReadsHuh(t *testing.T) {
	selectField := huh.NewSelect[string]().Options(huh.NewOptions("release-6.3", "main")...)
	multiField := huh.NewMultiSelect[string]().Options(huh.NewOptions("api", "web")...)
	for name, field := range map[string]huh.Field{"select": selectField, "multiselect": multiField} {
		query := filterQuery(field)
		if query == nil {
			t.Fatalf("%s: huh no longer has a textinput 'filter' field", name)
		}
		form := huh.NewForm(huh.NewGroup(field))
		form.Init()
		for _, msg := range keys("/rel") {
			form.Update(msg)
		}
		form.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		if got := query(); got != "re" {
			t.Errorf("%s: query = %q; want %q", name, got, "re")
		}
	}
	if filterQuery(huh.NewConfirm()) != nil {
		t.Error("confirm has no filter")
	}
}
