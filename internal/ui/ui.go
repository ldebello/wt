// Package ui provides the interactive prompts shared by every command:
// a filterable multi-select (repos, bundles, cleanup), a single select and a
// yes/no confirmation.
package ui

import (
	"errors"
	"os"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

// ErrNoTTY is returned when a prompt is needed but there is no terminal.
var ErrNoTTY = errors.New("interactive selection needs a terminal")

// ErrAborted is returned when the user cancels a prompt.
var ErrAborted = errors.New("aborted")

// Option is a selectable item.
type Option struct {
	Label    string
	Value    string
	Selected bool
}

// Prompter asks the user for input. Tests use a fake implementation.
type Prompter interface {
	MultiSelect(title string, options []Option) ([]string, error)
	Select(title string, options []Option) (string, error)
	Confirm(title string, def bool) (bool, error)
}

// Terminal is the huh-backed Prompter. It renders on stderr so commands whose
// stdout is captured (e.g. `wt cd`) can still prompt.
type Terminal struct{}

func (Terminal) MultiSelect(title string, options []Option) ([]string, error) {
	var values []string
	field := huh.NewMultiSelect[string]().
		Title(title).
		Options(huhOptions(options)...).
		Filterable(true).
		Value(&values)
	return values, run(field)
}

func (Terminal) Select(title string, options []Option) (string, error) {
	var value string
	field := huh.NewSelect[string]().
		Title(title).
		Options(huhOptions(options)...).
		Filtering(true).
		Value(&value)
	return value, run(field)
}

func (Terminal) Confirm(title string, def bool) (bool, error) {
	value := def
	field := huh.NewConfirm().Title(title).Affirmative("Yes").Negative("No").Value(&value)
	return value, run(field)
}

func huhOptions(options []Option) []huh.Option[string] {
	out := make([]huh.Option[string], len(options))
	for i, o := range options {
		out[i] = huh.NewOption(o.Label, o.Value).Selected(o.Selected)
	}
	return out
}

func run(field huh.Field) error {
	if !IsTerminal() {
		return ErrNoTTY
	}
	err := huh.NewForm(huh.NewGroup(field)).WithOutput(os.Stderr).WithShowHelp(true).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}

// IsTerminal reports whether stdin and stderr are terminals.
func IsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
}
