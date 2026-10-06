package ui

import (
	"os"
	"reflect"
	"strings"
	"unicode"
	"unsafe"

	"github.com/charmbracelet/bubbles/textinput"
)

// filterQuery returns a function that reads what the user typed in the
// filter of a huh Select or MultiSelect, or nil for other fields. huh keeps
// the filter in an unexported textinput.Model field; reading it is the only
// way to show exactly what was typed (the tests guard against huh changes).
func filterQuery(field any) func() string {
	v := reflect.ValueOf(field)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return nil
	}
	f := v.Elem().FieldByName("filter")
	if !f.IsValid() || f.Type() != reflect.TypeOf(textinput.Model{}) {
		return nil
	}
	input := (*textinput.Model)(unsafe.Pointer(f.UnsafeAddr()))
	return func() string { return input.Value() }
}

// highlightStyle marks the typed text: bold, underlined and, unless
// NO_COLOR is set, yellow.
func highlightStyle() string {
	if os.Getenv("NO_COLOR") != "" {
		return "\x1b[1;4m"
	}
	return "\x1b[1;4;38;5;220m"
}

// highlight marks, in a rendered view, the case-insensitive occurrences of
// query (the same matching huh's filter uses) inside the option labels and
// in the filter input itself, leaving titles and help untouched. Styles
// around a match are restored after it.
func highlight(view, query string, labels []string) string {
	q := lowerRunes(query)
	if len(q) == 0 {
		return view
	}
	// Only labels that contain the query can have matches.
	var candidates [][]rune
	for _, l := range labels {
		if ll := lowerRunes(l); indexRunes(ll, q, 0) >= 0 {
			candidates = append(candidates, ll)
		}
	}
	style := highlightStyle()

	var out strings.Builder
	active := "" // SGR sequences in effect, to restore after a match
	for i := 0; i < len(view); {
		if view[i] == 0x1b {
			j := escapeEnd(view, i)
			seq := view[i:j]
			out.WriteString(seq)
			if strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m") {
				if seq == "\x1b[0m" || seq == "\x1b[m" {
					active = ""
				} else {
					active += seq
				}
			}
			i = j
			continue
		}
		j := strings.IndexByte(view[i:], 0x1b)
		if j < 0 {
			j = len(view)
		} else {
			j += i
		}
		out.WriteString(highlightRun(view[i:j], query, q, candidates, style, active))
		i = j
	}
	return out.String()
}

// highlightRun marks the matches inside one run of unstyled text.
func highlightRun(run, query string, q []rune, labels [][]rune, style, active string) string {
	r := []rune(run)
	lr := lowerRunes(run)
	allowed := make([]bool, len(r))
	// The filter input: "/query" (or just the query, when huh styles the
	// prompt separately).
	if strings.EqualFold(strings.TrimSpace(run), query) {
		for k := range allowed {
			allowed[k] = true
		}
	}
	prompt := append([]rune{'/'}, q...)
	for at := indexRunes(lr, prompt, 0); at >= 0; at = indexRunes(lr, prompt, at+1) {
		for k := at + 1; k < at+len(prompt); k++ {
			allowed[k] = true
		}
	}
	for _, l := range labels {
		for at := indexRunes(lr, l, 0); at >= 0; at = indexRunes(lr, l, at+1) {
			for k := at; k < at+len(l); k++ {
				allowed[k] = true
			}
		}
	}

	var b strings.Builder
	last := 0
	for k := 0; k+len(q) <= len(lr); {
		if allowed[k] && allowed[k+len(q)-1] && equalRunes(lr[k:k+len(q)], q) {
			b.WriteString(string(r[last:k]))
			b.WriteString(style)
			b.WriteString(string(r[k : k+len(q)]))
			b.WriteString("\x1b[0m" + active)
			k += len(q)
			last = k
			continue
		}
		k++
	}
	b.WriteString(string(r[last:]))
	return b.String()
}

// escapeEnd returns the index just past the escape sequence starting at i.
func escapeEnd(s string, i int) int {
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[': // CSI: parameters, then a final byte in @..~
		for j := i + 2; j < len(s); j++ {
			if s[j] >= 0x40 && s[j] <= 0x7e {
				return j + 1
			}
		}
		return len(s)
	case ']': // OSC: ends with BEL or ESC \
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
		return len(s)
	}
	return i + 2
}

func lowerRunes(s string) []rune {
	r := []rune(s)
	for i, c := range r {
		r[i] = unicode.ToLower(c)
	}
	return r
}

func indexRunes(s, sub []rune, from int) int {
	for i := from; i+len(sub) <= len(s); i++ {
		if equalRunes(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func equalRunes(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
