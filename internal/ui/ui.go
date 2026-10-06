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

const (
	// GroupKey cycles the visible group in lists whose options have more
	// than one Group.
	GroupKey = "ctrl+t"
	// ChoiceKey opens the Choices dropdown of the highlighted option.
	ChoiceKey = "ctrl+b"
)

// Option is a selectable item.
type Option struct {
	Label    string
	Value    string
	Selected bool
	// Group allows filtering by kind (e.g. "workspaces", "repositories")
	// with GroupKey when a list mixes several groups.
	Group string
	// Choices, when set, lists alternatives for this option (e.g. its
	// branches); ChoiceKey opens them in a dropdown. The first choice is the
	// default. It is called lazily, only when the dropdown is opened.
	Choices    func() ([]Choice, error)
	ChoiceName string // what a choice is, for help text (e.g. "branch")
	// Choice is the picked choice value ("" = the default). It is appended to
	// the label in lists.
	Choice string
}

// Choice is one entry of an option's dropdown. A choice with Next opens a
// second dropdown, whose pick becomes the value.
type Choice struct {
	Label string
	Value string
	Next  []Choice
}

// Prompter asks the user for input. Tests use a fake implementation.
type Prompter interface {
	// MultiSelect returns the selected options, with any Choice made.
	MultiSelect(title string, options []Option) ([]Option, error)
	Select(title string, options []Option) (string, error)
	Confirm(title string, def bool) (bool, error)
}

// Terminal is the huh-backed Prompter. It renders on stderr so commands whose
// stdout is captured (e.g. `wt cd`) can still prompt.
type Terminal struct{}

func (Terminal) MultiSelect(title string, options []Option) ([]Option, error) {
	options = slices.Clone(options)
	groups := Groups(options)
	view := 0
	for {
		visible := Visible(options, groups, view)
		var values []string
		field := huh.NewMultiSelect[string]().
			Title(title).
			Description(help(options, groups, view)).
			Options(huhOptions(visible)...).
			Filterable(true).
			Value(&values)
		key, hovered, err := run(field, field.Hovered, false)
		if err != nil {
			return nil, err
		}
		// Remember choices made in this view, keep hidden ones as they were.
		for i := range options {
			if slices.ContainsFunc(visible, func(o Option) bool { return o.Value == options[i].Value }) {
				options[i].Selected = slices.Contains(values, options[i].Value)
			}
		}
		switch key {
		case GroupKey:
			view = (view + 1) % (len(groups) + 1)
		case ChoiceKey:
			if err := chooseFor(options, hovered); err != nil {
				return nil, err
			}
		default:
			return SelectedOptions(options), nil
		}
	}
}

// chooseFor opens the dropdown of the option with value hovered. Picking a
// choice also selects the option.
func chooseFor(options []Option, hovered string) error {
	i := slices.IndexFunc(options, func(o Option) bool { return o.Value == hovered })
	if i < 0 || options[i].Choices == nil {
		return nil
	}
	choices, err := options[i].Choices()
	if err != nil {
		return err
	}
	items := make([]Option, len(choices))
	for j, c := range choices {
		items[j] = Option{Label: c.Label, Value: c.Value}
	}
	name := options[i].ChoiceName
	if name == "" {
		name = "option"
	}
	label := strings.TrimSpace(options[i].Label)
	picked, err := Terminal{}.Select(label+": choose "+name, items)
	if errors.Is(err, ErrAborted) {
		return nil // Esc in the dropdown just goes back to the list
	}
	if err != nil {
		return err
	}
	j := slices.IndexFunc(choices, func(c Choice) bool { return c.Value == picked })
	if j >= 0 && len(choices[j].Next) > 0 {
		next := make([]Option, len(choices[j].Next))
		for k, c := range choices[j].Next {
			next[k] = Option{Label: c.Label, Value: c.Value}
		}
		picked, err = Terminal{}.Select(label+": "+strings.TrimSpace(choices[j].Label), next)
		if errors.Is(err, ErrAborted) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	options[i].Choice = picked
	options[i].Selected = true
	return nil
}

func (Terminal) Select(title string, options []Option) (string, error) {
	groups := Groups(options)
	for view := 0; ; view = (view + 1) % (len(groups) + 1) {
		var value string
		field := huh.NewSelect[string]().
			Title(title).
			Description(strings.TrimSpace(help(options, groups, view) + "   type to filter")).
			Options(huhOptions(Visible(options, groups, view))...).
			Value(&value)
		key, _, err := run(field, nil, true)
		if err != nil || key != GroupKey {
			return value, err
		}
	}
}

func (Terminal) Confirm(title string, def bool) (bool, error) {
	value := def
	field := huh.NewConfirm().Title(title).Affirmative("Yes").Negative("No").Value(&value)
	_, _, err := run(field, nil, false)
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

// SelectedOptions returns the selected options, in order.
func SelectedOptions(options []Option) []Option {
	var out []Option
	for _, o := range options {
		if o.Selected {
			out = append(out, o)
		}
	}
	return out
}

// Values returns the values of options.
func Values(options []Option) []string {
	out := make([]string, len(options))
	for i, o := range options {
		out[i] = o.Value
	}
	return out
}

// DisplayLabel is the label shown in lists, followed by the choice.
func (o Option) DisplayLabel() string {
	return o.Label + o.Choice
}

// help describes the extra keys available for options.
func help(options []Option, groups []string, view int) string {
	var parts []string
	if len(groups) > 0 {
		labels := append([]string{"all"}, groups...)
		for i, l := range labels {
			if i == view {
				labels[i] = "[" + l + "]"
			}
		}
		parts = append(parts, GroupKey+": "+strings.Join(labels, " / "))
	}
	if i := slices.IndexFunc(options, func(o Option) bool { return o.Choices != nil }); i >= 0 {
		name := options[i].ChoiceName
		if name == "" {
			name = "option"
		}
		parts = append(parts, ChoiceKey+": choose "+name)
	}
	return strings.Join(parts, "   ")
}

func huhOptions(options []Option) []huh.Option[string] {
	out := make([]huh.Option[string], len(options))
	for i, o := range options {
		out[i] = huh.NewOption(o.DisplayLabel(), o.Value).Selected(o.Selected)
	}
	return out
}

// run shows a single-field form. It returns the extra key (GroupKey or
// ChoiceKey) that ended it, if any, and the hovered value at that moment.
//
// With typeToFilter, typing a character starts filtering right away (as if
// '/' had been pressed first); used for single selects, where letters have
// no other meaning.
func run(field huh.Field, hovered func() (string, bool), typeToFilter bool) (key, hoveredValue string, err error) {
	if !IsTerminal() {
		return "", "", ErrNoTTY
	}
	form := huh.NewForm(huh.NewGroup(field)).WithShowHelp(true)
	form.SubmitCmd = tea.Quit
	form.CancelCmd = tea.Interrupt
	m := &keyCatcher{form: form, hovered: hovered, typeToFilter: typeToFilter}
	_, err = tea.NewProgram(m, tea.WithOutput(os.Stderr)).Run()
	if errors.Is(err, tea.ErrInterrupted) || form.State == huh.StateAborted {
		return "", "", ErrAborted
	}
	return m.key, m.hoveredValue, err
}

// keyCatcher wraps a form to intercept GroupKey and ChoiceKey, and to start
// filtering when the user types.
type keyCatcher struct {
	form         *huh.Form
	hovered      func() (string, bool)
	typeToFilter bool
	filtering    bool
	key          string
	hoveredValue string
}

func (m *keyCatcher) Init() tea.Cmd { return m.form.Init() }

func (m *keyCatcher) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case GroupKey:
			m.key = GroupKey
			return m, tea.Quit
		case ChoiceKey:
			if m.hovered != nil {
				if v, ok := m.hovered(); ok {
					m.key, m.hoveredValue = ChoiceKey, v
					return m, tea.Quit
				}
			}
			return m, nil
		}
		if m.typeToFilter {
			switch {
			case k.Type == tea.KeyRunes && !k.Alt && !m.filtering:
				m.filtering = true
				if k.String() != "/" {
					// Open the filter, then type the key into it.
					_, open := m.form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
					_, typed := m.form.Update(msg)
					return m, tea.Batch(open, typed)
				}
			case k.Type == tea.KeyEsc || k.Type == tea.KeyEnter:
				m.filtering = false
			}
		}
	}
	_, cmd := m.form.Update(msg)
	return m, cmd
}

func (m *keyCatcher) View() string {
	if m.key != "" {
		return ""
	}
	return m.form.View()
}

// IsTerminal reports whether stdin and stderr are terminals.
func IsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
}
