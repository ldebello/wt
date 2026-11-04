package workspace

import (
	"fmt"
	"strings"
)

// Spec is a requested repository:
//
//	repo         the workspace branch (existing, or new from the default branch)
//	repo@branch  work directly on an existing branch
//	repo:base    the workspace branch, created from base if it does not exist
//	repo#ro      read-only: a detached checkout of origin/<default>, no branch
//	             of its own, never touched by 'wt ws rm'. Combine with
//	             repo@branch#ro to pin it to a specific branch instead.
//
// At most one of Branch and Base is set; ReadOnly never combines with Base
// (a base is for a branch of your own, which is for writing).
type Spec struct {
	Repo     string
	Branch   string // check out this branch instead of the workspace branch
	Base     string // start point for a new workspace branch
	ReadOnly bool   // a detached checkout, context only: see repo#ro above
}

func (s Spec) String() string {
	suffix := ""
	if s.ReadOnly {
		suffix = "#ro"
	}
	switch {
	case s.Branch != "":
		return s.Repo + "@" + s.Branch + suffix
	case s.Base != "":
		return s.Repo + ":" + s.Base
	}
	return s.Repo + suffix
}

const specSyntax = "expected repo, repo@branch (work on branch), repo:base (new workspace branch from base) or repo#ro (read-only, for context)"

// ParseSpec parses "repo", "repo@branch", "repo:base", or any of the first
// two with a trailing "#ro". Git branch names cannot contain ':' or '#', so
// the separators are unambiguous.
func ParseSpec(s string) (Spec, error) {
	s = strings.TrimSpace(s)
	readOnly := false
	if rest, ok := strings.CutSuffix(s, "#ro"); ok {
		readOnly, s = true, rest
	}
	i := strings.IndexAny(s, "@:")
	if i < 0 {
		if s == "" {
			return Spec{}, fmt.Errorf("invalid repository %q: %s", s, specSyntax)
		}
		return Spec{Repo: s, ReadOnly: readOnly}, nil
	}
	repo, sep, rest := s[:i], s[i], s[i+1:]
	if repo == "" || rest == "" || strings.ContainsAny(rest, ":#") {
		return Spec{}, fmt.Errorf("invalid repository %q: %s", s, specSyntax)
	}
	if sep == '@' {
		return Spec{Repo: repo, Branch: rest, ReadOnly: readOnly}, nil
	}
	if readOnly {
		return Spec{}, fmt.Errorf("invalid repository %q: a base is for a branch of your own to write on; it cannot be read-only (#ro)", repo+":"+rest+"#ro")
	}
	return Spec{Repo: repo, Base: rest}, nil
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
//   - an explicit repo@branch or repo:base overrides any bundle;
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
		case prev.Branch == spec.Branch && prev.Base == spec.Base && prev.ReadOnly == spec.ReadOnly:
			return
		case override && (spec.Branch != "" || spec.Base != ""):
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
	label := "the workspace branch"
	switch {
	case s.Branch != "":
		label = "branch " + s.Branch
	case s.Base != "":
		label = "a new workspace branch from " + s.Base
	}
	if s.ReadOnly {
		label += " (read-only)"
	}
	return label
}
