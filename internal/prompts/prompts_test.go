package prompts

import (
	"testing"
)

// Every prompt file parses as a template (Render's panics are found here).
func TestEveryPromptParses(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("no prompt embedded")
	}
	for _, n := range names {
		if Text(n) == "" {
			t.Errorf("%s is empty", n)
		}
		parsed(n)
	}
}
