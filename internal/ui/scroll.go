package ui

import (
	"os"
	"reflect"
	"unsafe"

	"github.com/charmbracelet/bubbles/viewport"
	"golang.org/x/term"
)

// listHeight bounds a Select/MultiSelect to the terminal height, so a long
// list scrolls within view instead of some of it running off past the
// terminal with no sign it's there (see scrollHint, which marks the part
// that's left offscreen). 0 (the huh default: size to fit the content) is
// returned when the content already fits, or the terminal size is unknown,
// so short lists are never padded out to a taller field than they need.
func listHeight(n int) int {
	_, rows, err := term.GetSize(int(os.Stderr.Fd()))
	if err != nil || rows <= 0 {
		return 0
	}
	// Height() only budgets the title, description and options: the form's
	// own keybinding help line and our scrollIndicator line are rendered
	// outside that budget, so they must be reserved here too, plus a couple
	// of rows so the prompt isn't flush against the edge of the terminal.
	const reserved = 4
	budget := rows - reserved
	if n <= budget {
		return 0
	}
	return budget
}

// scrollHint returns a function reading whether a huh Select or MultiSelect
// is scrolled away from the top and/or bottom of its list, by peeking at its
// unexported viewport and filteredOptions fields — the same technique and
// caveat as filterQuery: nil for other fields, or if a huh update changes
// this shape.
//
// It does its own bottom/length math (YOffset+Height vs. len(filtered))
// rather than using viewport.Model's own AtBottom/TotalLineCount: huh v1.0.0
// pads the rendered options to the field's *unfiltered* option count, so
// those would stay too tall, and over-report "more below", once a filter
// narrows the list.
func scrollHint(field any) func() (atTop, atBottom bool) {
	v := reflect.ValueOf(field)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return nil
	}
	vf := v.Elem().FieldByName("viewport")
	ff := v.Elem().FieldByName("filteredOptions")
	if !vf.IsValid() || vf.Type() != reflect.TypeOf(viewport.Model{}) || !ff.IsValid() || ff.Kind() != reflect.Slice {
		return nil
	}
	vp := (*viewport.Model)(unsafe.Pointer(vf.UnsafeAddr()))
	filtered := reflect.NewAt(ff.Type(), unsafe.Pointer(ff.UnsafeAddr())).Elem()
	return func() (bool, bool) {
		return vp.YOffset <= 0, vp.YOffset+vp.Height >= filtered.Len()
	}
}

// scrollIndicator marks, below the rendered list, that it is scrolled: there
// is more above and/or below what's currently visible. "" when the whole
// list already fits on screen (atTop && atBottom).
func scrollIndicator(atTop, atBottom bool) string {
	switch {
	case atTop && atBottom:
		return ""
	case atTop:
		return "\n↓ more below"
	case atBottom:
		return "\n↑ more above"
	default:
		return "\n↑ more above    ↓ more below"
	}
}
