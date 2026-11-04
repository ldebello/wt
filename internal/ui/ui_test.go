package ui

import (
	"reflect"
	"testing"
)

func TestGroupsAndVisible(t *testing.T) {
	options := []Option{
		{Value: "ws1", Group: "workspace"},
		{Value: "r1", Group: "repository", Selected: true},
		{Value: "ws2", Group: "workspace", Selected: true},
	}
	groups := Groups(options)
	if !reflect.DeepEqual(groups, []string{"workspace", "repository"}) {
		t.Fatalf("Groups = %v", groups)
	}
	values := func(opts []Option) []string {
		var out []string
		for _, o := range opts {
			out = append(out, o.Value)
		}
		return out
	}
	if got := values(Visible(options, groups, 0)); len(got) != 3 {
		t.Errorf("view all = %v", got)
	}
	if got := values(Visible(options, groups, 1)); !reflect.DeepEqual(got, []string{"ws1", "ws2"}) {
		t.Errorf("view workspaces = %v", got)
	}
	if got := values(Visible(options, groups, 2)); !reflect.DeepEqual(got, []string{"r1"}) {
		t.Errorf("view repositories = %v", got)
	}
	if got := Values(SelectedOptions(options)); !reflect.DeepEqual(got, []string{"r1", "ws2"}) {
		t.Errorf("SelectedOptions = %v", got)
	}

	// A single group needs no filtering.
	if Groups([]Option{{Group: "a"}, {Group: "a"}, {}}) != nil {
		t.Error("expected nil groups")
	}
}

func TestHelpAndLabels(t *testing.T) {
	plain := []Option{{Value: "a"}}
	if help(plain, nil, 0) != "" {
		t.Error("expected no help without groups or choices")
	}
	withChoices := []Option{{Value: "a"}, {Value: "b", ChoiceName: "branch", Choices: func() ([]Choice, error) { return nil, nil }}}
	if got := help(withChoices, []string{"workspace", "repository"}, 1); got != "ctrl+t: all / [workspace] / repository   ctrl+b: choose branch   ctrl+r: toggle read-only" {
		t.Errorf("help = %q", got)
	}
	if got := (Option{Label: "api"}).DisplayLabel(); got != "api" {
		t.Errorf("DisplayLabel = %q", got)
	}
	if got := (Option{Label: "api", Choice: ":release"}).DisplayLabel(); got != "api:release" {
		t.Errorf("DisplayLabel = %q", got)
	}
	if got := (Option{Label: "api", ReadOnly: true}).DisplayLabel(); got != "api  (read-only)" {
		t.Errorf("DisplayLabel = %q", got)
	}
}

func TestScrollHintDegradesGracefully(t *testing.T) {
	// Not a pointer, or a field shape scrollHint doesn't recognize: nil,
	// same as filterQuery, rather than a panic if huh's internals change.
	if scrollHint(struct{}{}) != nil {
		t.Error("expected nil for a non-pointer")
	}
	if scrollHint(&struct{ viewport int }{}) != nil {
		t.Error("expected nil when viewport isn't a viewport.Model")
	}
}

func TestScrollIndicator(t *testing.T) {
	cases := []struct {
		atTop, atBottom bool
		want            string
	}{
		{true, true, ""},
		{true, false, "\n↓ more below"},
		{false, true, "\n↑ more above"},
		{false, false, "\n↑ more above    ↓ more below"},
	}
	for _, c := range cases {
		if got := scrollIndicator(c.atTop, c.atBottom); got != c.want {
			t.Errorf("scrollIndicator(%v, %v) = %q; want %q", c.atTop, c.atBottom, got, c.want)
		}
	}
}

func TestToggleReadOnly(t *testing.T) {
	choices := func() ([]Choice, error) { return nil, nil }
	options := []Option{
		{Value: "nochoices"},
		{Value: "api", Choices: choices},
		{Value: "based", Choices: choices, Choice: ":release"},
	}

	// Only applies to options with Choices (like ChoiceKey).
	toggleReadOnly(options, "nochoices")
	if options[0].ReadOnly {
		t.Error("toggled an option without Choices")
	}

	// Toggling on selects it.
	toggleReadOnly(options, "api")
	if !options[1].ReadOnly || !options[1].Selected {
		t.Errorf("api = %+v", options[1])
	}
	toggleReadOnly(options, "api")
	if options[1].ReadOnly {
		t.Error("did not toggle off")
	}

	// Exclusive with a ":base" choice (a branch of your own to write on): it
	// clears the choice.
	toggleReadOnly(options, "based")
	if !options[2].ReadOnly || options[2].Choice != "" {
		t.Errorf("based = %+v", options[2])
	}
}
