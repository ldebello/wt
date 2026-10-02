package workspace

import (
	"fmt"
	"strings"
)

// Spec is a requested repository with an optional branch ("repo@branch").
// An empty Branch means "use the workspace name".
type Spec struct {
	Repo   string
	Branch string
}

func (s Spec) String() string {
	if s.Branch == "" {
		return s.Repo
	}
	return s.Repo + "@" + s.Branch
}

// ParseSpec parses "repo" or "repo@branch".
func ParseSpec(s string) (Spec, error) {
	repo, branch, hasBranch := strings.Cut(strings.TrimSpace(s), "@")
	if repo == "" || (hasBranch && branch == "") {
		return Spec{}, fmt.Errorf("invalid repository %q: expected repo or repo@branch", s)
	}
	return Spec{Repo: repo, Branch: branch}, nil
}

// ParseSpecs parses a list of specs, also splitting comma-separated items.
func ParseSpecs(items []string) ([]Spec, error) {
	var specs []Spec
	for _, item := range items {
		for _, part := range strings.Split(item, ",") {
			if strings.TrimSpace(part) == "" {
				continue
			}
			spec, err := ParseSpec(part)
			if err != nil {
				return nil, err
			}
			specs = append(specs, spec)
		}
	}
	return specs, nil
}

// NamedSpecs is a bundle's specs, kept with the bundle name for logging.
type NamedSpecs struct {
	Name  string
	Specs []Spec
}

// Combine merges bundle specs and explicit specs into one list, one entry per
// repository, in order of first appearance:
//   - the set of repositories is the union of all bundles and explicit specs;
//   - an explicit repo@branch overrides any bundle;
//   - when bundles disagree on a branch, the first bundle wins.
//
// Every automatic resolution is described in the returned notes.
func Combine(bundles []NamedSpecs, explicit []Spec) ([]Spec, []string) {
	var order []string
	chosen := map[string]Spec{}
	source := map[string]string{}
	var notes []string

	add := func(spec Spec, from string, override bool) {
		prev, seen := chosen[spec.Repo]
		switch {
		case !seen:
			order = append(order, spec.Repo)
		case prev.Branch == spec.Branch:
			return
		case override && spec.Branch != "":
			notes = append(notes, fmt.Sprintf("%s: using %s from %s over %s from %s", spec.Repo, branchLabel(spec), from, branchLabel(prev), source[spec.Repo]))
		default:
			notes = append(notes, fmt.Sprintf("%s: keeping %s from %s, ignoring %s from %s", spec.Repo, branchLabel(prev), source[spec.Repo], branchLabel(spec), from))
			return
		}
		chosen[spec.Repo] = spec
		source[spec.Repo] = from
	}
	for _, b := range bundles {
		for _, spec := range b.Specs {
			add(spec, "bundle "+b.Name, false)
		}
	}
	for _, spec := range explicit {
		add(spec, "--repos", true)
	}

	specs := make([]Spec, len(order))
	for i, repo := range order {
		specs[i] = chosen[repo]
	}
	return specs, notes
}

func branchLabel(s Spec) string {
	if s.Branch == "" {
		return "the workspace branch"
	}
	return "branch " + s.Branch
}
