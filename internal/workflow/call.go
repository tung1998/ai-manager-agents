package workflow

import (
	"regexp"
	"strings"
)

// Prefix calls a workflow from a chat or an automation ("#key input"), apart
// from "/" (skills) and "@" (agents). "/key" still runs a workflow, as before.
const Prefix = "#"

var callRe = regexp.MustCompile(`^#([A-Za-z0-9][A-Za-z0-9._:-]*)(?:\s+|$)`)

// ParseCall splits "#key rest" into key and rest. ok is false when the text
// is not a workflow call.
func ParseCall(text string) (key, rest string, ok bool) {
	m := callRe.FindStringSubmatchIndex(text)
	if m == nil {
		return "", text, false
	}
	return text[m[2]:m[3]], strings.TrimSpace(text[m[1]:]), true
}

// Call is the text that runs workflow key with input.
func Call(key, input string) string {
	return strings.TrimSpace(Prefix + key + " " + strings.TrimSpace(input))
}
