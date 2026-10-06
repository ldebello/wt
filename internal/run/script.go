// Package run executes shell scripts in the repositories of a workspace and
// expands the user's shell aliases in those scripts.
package run

import (
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

// ParseAliases reads the output of `alias` from zsh (name=value) or bash
// (alias name='value').
func ParseAliases(out string) map[string]string {
	aliases := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "alias ")
		name, value, ok := strings.Cut(line, "=")
		if !ok || name == "" {
			continue
		}
		aliases[Unquote(name)] = Unquote(value)
	}
	return aliases
}

// Unquote removes shell quoting from a single word: '...', "..." and
// backslash escapes.
func Unquote(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				b.WriteString(s[i+1:])
				return b.String()
			}
			b.WriteString(s[i+1 : i+1+j])
			i += j + 1
		case '"':
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`", s[i+1]) >= 0 {
					i++
				}
				b.WriteByte(s[i])
			}
		case '\\':
			if i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// keywords keep the next word in command position.
var keywords = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "do": true, "while": true,
	"until": true, "!": true, "{": true, "time": true, "exec": true, "command": true,
	"nohup": true,
}

// endKeywords are reserved words that are not commands.
var endKeywords = map[string]bool{"fi": true, "done": true, "esac": true, "}": true, "in": true, "for": true, "case": true}

// Expand replaces aliases used as commands in script (at the start, after
// ;, &&, ||, |, &, (, newlines, keywords and inside $(...) or `...`), the
// way an interactive shell would. Quoted words are never expanded. It also
// returns every command word of the result.
func Expand(script string, aliases map[string]string) (string, []string) {
	e := expander{aliases: aliases}
	out := e.expand(script, map[string]bool{})
	return out, e.words
}

type expander struct {
	aliases map[string]string
	words   []string
}

func (e *expander) expand(s string, seen map[string]bool) string {
	var b strings.Builder
	atCmd := true
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			b.WriteByte(c)
			i++
		case c == '\n' || c == ';' || c == '&' || c == '|' || c == '(':
			b.WriteByte(c)
			i++
			atCmd = true
		case c == ')':
			b.WriteByte(c)
			i++
			atCmd = false
		case c == '<' || c == '>':
			// A redirection: its target is a file name, not a command.
			for i < len(s) && (s[i] == '<' || s[i] == '>' || s[i] == '&') {
				b.WriteByte(s[i])
				i++
			}
			for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
				b.WriteByte(s[i])
				i++
			}
			word, n := e.readWord(s[i:], seen)
			b.WriteString(word)
			i += n
		default:
			raw, n := e.readWord(s[i:], seen)
			i += n
			if !atCmd {
				b.WriteString(raw)
				continue
			}
			plain := Unquote(raw)
			switch {
			case isAssignment(raw):
				b.WriteString(raw) // FOO=bar cmd: still in command position
			case keywords[plain]:
				b.WriteString(raw)
			case endKeywords[plain]:
				b.WriteString(raw)
				atCmd = false
			default:
				value, isAlias := e.aliases[raw]
				if isAlias && !seen[raw] && raw == plain {
					inner := map[string]bool{raw: true}
					for k := range seen {
						inner[k] = true
					}
					b.WriteString(e.expand(value, inner))
				} else {
					if !strings.ContainsAny(raw, "$`") { // a computed command can't be checked
						e.words = append(e.words, plain)
					}
					b.WriteString(raw)
				}
				atCmd = false
			}
		}
	}
	return b.String()
}

// readWord reads one shell word, expanding aliases inside $(...) and `...`.
// It returns the (possibly rewritten) word and how many bytes it consumed.
func (e *expander) readWord(s string, seen map[string]bool) (string, int) {
	var b strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case strings.IndexByte(" \t\n;&|()<>", c) >= 0:
			return b.String(), i
		case c == '\\':
			end := min(i+2, len(s))
			b.WriteString(s[i:end])
			i = end
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			end := len(s)
			if j >= 0 {
				end = i + 2 + j
			}
			b.WriteString(s[i:end])
			i = end
		case c == '"':
			b.WriteByte(c)
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					b.WriteString(s[i : i+2])
					i += 2
					continue
				}
				if sub, n := e.substitution(s[i:], seen); n > 0 {
					b.WriteString(sub)
					i += n
					continue
				}
				b.WriteByte(s[i])
				i++
			}
			if i < len(s) {
				b.WriteByte('"')
				i++
			}
		default:
			if sub, n := e.substitution(s[i:], seen); n > 0 {
				b.WriteString(sub)
				i += n
				continue
			}
			b.WriteByte(c)
			i++
		}
	}
	return b.String(), i
}

// substitution expands a $(...) or `...` command substitution at the start
// of s. It returns 0 bytes consumed when there is none.
func (e *expander) substitution(s string, seen map[string]bool) (string, int) {
	switch {
	case strings.HasPrefix(s, "$(("):
		end := matching(s, 3, '(', ')')
		end = min(end+1, len(s)) // arithmetic: $(( ... )) is left alone
		return s[:end], end
	case strings.HasPrefix(s, "$("):
		end := matching(s, 2, '(', ')')
		return "$(" + e.expand(s[2:end], seen) + s[end:min(end+1, len(s))], min(end+1, len(s))
	case strings.HasPrefix(s, "`"):
		j := strings.IndexByte(s[1:], '`')
		if j < 0 {
			return s, len(s)
		}
		return "`" + e.expand(s[1:1+j], seen) + "`", j + 2
	}
	return "", 0
}

// matching returns the index of the bracket closing the one before start,
// skipping quoted text.
func matching(s string, start int, open, close byte) int {
	depth := 1
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '\'':
			if j := strings.IndexByte(s[i+1:], '\''); j >= 0 {
				i += j + 1
			}
		case open:
			depth++
		case close:
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return len(s)
}

func isAssignment(word string) bool {
	name, _, ok := strings.Cut(word, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// builtins are shell builtins and keywords that are always available.
var builtins = []string{
	".", ":", "[", "alias", "bg", "break", "builtin", "cd", "command", "continue",
	"echo", "eval", "exec", "exit", "export", "false", "fg", "getopts", "hash",
	"jobs", "kill", "let", "local", "printf", "pwd", "read", "readonly", "return",
	"set", "shift", "source", "test", "times", "trap", "true", "type", "typeset",
	"ulimit", "umask", "unalias", "unset", "wait",
}

// Unresolved returns the command words that are neither programs on PATH,
// paths, variables nor builtins: shell functions or typos, which a
// non-interactive shell can't run.
func Unresolved(words []string) []string {
	var out []string
	for _, w := range words {
		if w == "" || strings.ContainsAny(w, "/$") || slices.Contains(builtins, w) || slices.Contains(out, w) {
			continue
		}
		if _, err := exec.LookPath(w); err != nil {
			out = append(out, w)
		}
	}
	return out
}

// Params reports the highest positional parameter a script uses ($1, ${2})
// and whether it uses all of them ($@ or $*). Text in single quotes and
// escaped dollars are ignored, as the shell would.
func Params(script string) (max int, variadic bool) {
	for i := 0; i < len(script); i++ {
		switch script[i] {
		case '\\':
			i++
		case '\'':
			if j := strings.IndexByte(script[i+1:], '\''); j >= 0 {
				i += j + 1
			}
		case '$':
			rest := script[i+1:]
			braced := strings.HasPrefix(rest, "{")
			if braced {
				rest = rest[1:]
			}
			if rest != "" && (rest[0] == '@' || rest[0] == '*') {
				variadic = true
				continue
			}
			digits := 0
			for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' && (braced || digits == 0) {
				digits++
			}
			if n, err := strconv.Atoi(rest[:digits]); err == nil && n > max {
				max = n
			}
		}
	}
	return max, variadic
}
