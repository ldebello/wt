// Package ui provides the interactive prompts shared by every command:
// a filterable multi-select (repos, bundles, cleanup), a single select and a
// yes/no confirmation.
package ui

import (
	"errors"
	"os"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

// ErrNoTTY is returned when a prompt is needed but there is no terminal.
var ErrNoTTY = errors.New("interactive selection needs a terminal")

// ErrAborted is returned when the user cancels a prompt.
var ErrAborted = errors.New("aborted")

// GroupKey cycles the visible group in selects whose options have more than
// one Group.
const GroupKey = "ctrl+t"

// Option is a selectable item. Options with different Groups (e.g.
// "workspace", "repository") can be filtered by group with GroupKey.
type Option struct {
	Label    string
	Value    string
	Selected bool
	Group    string
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
	options = slices.Clone(options)
	groups := Groups(options)
	for view := 0; ; view = (view + 1) % (len(groups) + 1) {
		visible := Visible(options, groups, view)
		var values []string
		field := huh.NewMultiSelect[string]().
			Title(title).
			Description(groupHelp(groups, view)).
			Options(huhOptions(visible)...).
			Filterable(true).
			Value(&values)
		toggled, err := run(field)
		if err != nil {
			return nil, err
		}
		// Remember choices made in this view, keep hidden ones as they were.
		for i := range options {
			if slices.ContainsFunc(visible, func(o Option) bool { return o.Value == options[i].Value }) {
				options[i].Selected = slices.Contains(values, options[i].Value)
			}
		}
		if !toggled {
			return SelectedValues(options), nil
		}
	}
}

func (Terminal) Select(title string, options []Option) (string, error) {
	groups := Groups(options)
	for view := 0; ; view = (view + 1) % (len(groups) + 1) {
		var value string
		field := huh.NewSelect[string]().
			Title(title).
			Description(groupHelp(groups, view)).
			Options(huhOptions(Visible(options, groups, view))...).
			Filtering(true).
			Value(&value)
		toggled, err := run(field)
		if err != nil || !toggled {
			return value, err
		}
	}
}

func (Terminal) Confirm(title string, def bool) (bool, error) {
	value := def
	field := huh.NewConfirm().Title(title).Affirmative("Yes").Negative("No").Value(&value)
	_, err := run(field)
	return value, err
}

// Groups returns the distinct option groups in order of appearance, or nil
// when there is at most one (no filtering needed).
func Groups(options []Option) []string {
	var groups []string
	for _, o := range options {
		if o.Group != "" && !slices.Contains(groups, o.Group) {
			groups = append(groups, o.Group)
		}
	}
	if len(groups) < 2 {
		return nil
	}
	return groups
}

// Visible returns the options shown in view: 0 is all, i shows groups[i-1].
func Visible(options []Option, groups []string, view int) []Option {
	if view == 0 {
		return options
	}
	var out []Option
	for _, o := range options {
		if o.Group == groups[view-1] {
			out = append(out, o)
		}
	}
	return out
}

// SelectedValues returns the values of the selected options, in order.
func SelectedValues(options []Option) []string {
	var out []string
	for _, o := range options {
		if o.Selected {
			out = append(out, o.Value)
		}
	}
	return out
}

func groupHelp(groups []string, view int) string {
	if len(groups) == 0 {
		return ""
	}
	labels := append([]string{"all"}, groups...)
	for i, l := range labels {
		if i == view {
			labels[i] = "[" + l + "]"
		}
	}
	return GroupKey + ": " + strings.Join(labels, " / ")
}

func huhOptions(options []Option) []huh.Option[string] {
	out := make([]huh.Option[string], len(options))
	for i, o := range options {
		out[i] = huh.NewOption(o.Label, o.Value).Selected(o.Selected)
	}
	return out
}

// run shows a single-field form. It reports toggled=true when the user
// pressed GroupKey, so the caller can show the next group.
func run(field huh.Field) (toggled bool, err error) {
	if !IsTerminal() {
		return false, ErrNoTTY
	}
	form := huh.NewForm(huh.NewGroup(field)).WithShowHelp(true)
	form.SubmitCmd = tea.Quit
	form.CancelCmd = tea.Interrupt
	m := &groupToggle{form: form}
	_, err = tea.NewProgram(m, tea.WithOutput(os.Stderr)).Run()
	if errors.Is(err, tea.ErrInterrupted) || form.State == huh.StateAborted {
		return false, ErrAborted
	}
	return m.toggled, err
}

// groupToggle wraps a form to intercept GroupKey.
type groupToggle struct {
	form    *huh.Form
	toggled bool
}

func (m *groupToggle) Init() tea.Cmd { return m.form.Init() }

func (m *groupToggle) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.String() == GroupKey {
		m.toggled = true
		return m, tea.Quit
	}
	_, cmd := m.form.Update(msg)
	return m, cmd
}

func (m *groupToggle) View() string {
	if m.toggled {
		return ""
	}
	return m.form.View()
}

// IsTerminal reports whether stdin and stderr are terminals.
func IsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
}
