package cmd

import (
	"regexp"
	"strings"
)

// BlockParameter may be implemented by a Parameter that takes a block with its states, such as /setblock's block
// and /fill's replace filter: a block name with Java Edition's states attached (repeater[delay=2]) or followed by
// Bedrock Edition's (unpowered_repeater ["repeater_delay"=2]). Bedrock clients are sent an optional block states
// argument after it, so that they accept the states; Java clients complete it with their own block state
// argument, a block predicate when BlockPredicate returns true.
type BlockParameter interface {
	Parameter
	BlockPredicate() bool
}

// quotedState matches a quoted Bedrock block state name followed by = or :.
var quotedState = regexp.MustCompile(`"[ \t]*[=:]`)

// compactBlockStates removes the spaces outside quotes from bracketed Bedrock block states in a command line,
// such as ["minecraft:cardinal_direction" = "north", "repeater_delay"=2], so that they stay one argument: a word
// starting with a quote would otherwise be read as a quoted argument up to the end of the line.
func compactBlockStates(args string) string {
	if !strings.Contains(args, "[") || !strings.Contains(args, `"`) {
		return args
	}
	var b strings.Builder
	quoted := false
	for i := 0; i < len(args); i++ {
		c := args[i]
		if c == '"' {
			quoted = !quoted
		} else if c == '[' && !quoted {
			if end := bracketEnd(args, i); end > 0 && quotedState.MatchString(args[i:end+1]) {
				b.WriteString(withoutSpaces(args[i : end+1]))
				i = end
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// bracketEnd returns the index of the ] closing the [ at args[start], skipping quoted text, or -1.
func bracketEnd(args string, start int) int {
	depth, quoted := 0, false
	for i := start; i < len(args); i++ {
		switch c := args[i]; {
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '[':
			depth++
		case c == ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// withoutSpaces removes the spaces and tabs outside quotes from s.
func withoutSpaces(s string) string {
	var b strings.Builder
	quoted := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			quoted = !quoted
		}
		if !quoted && (c == ' ' || c == '\t') {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
