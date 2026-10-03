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
	if got := help(withChoices, []string{"workspace", "repository"}, 1); got != "ctrl+t: all / [workspace] / repository   ctrl+b: choose branch" {
		t.Errorf("help = %q", got)
	}
	if got := (Option{Label: "api"}).DisplayLabel(); got != "api" {
		t.Errorf("DisplayLabel = %q", got)
	}
	if got := (Option{Label: "api", Choice: "feature/x"}).DisplayLabel(); got != "api @ feature/x" {
		t.Errorf("DisplayLabel = %q", got)
	}
}
