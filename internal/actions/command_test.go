package actions

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRunCommandUTF8Truncation(t *testing.T) {
	// Build output whose last commandOutput bytes start exactly inside the
	// 3-byte encoding of "ệ", so a byte-based tail cut slices the rune in
	// half; a rune-based cut must not.
	ch := "ệ"
	prefix := strings.Repeat("a", 100)
	suffix := strings.Repeat("x", commandOutput+1-len(ch))
	text := prefix + ch + suffix
	out, err := runCommand(context.Background(), ".", `echo "`+text+`"`)
	if err != nil {
		t.Fatalf("runCommand: %v", err)
	}
	if !utf8.ValidString(out) {
		t.Fatalf("output is not valid UTF-8: %q", out)
	}
	if strings.ContainsRune(out, utf8.RuneError) {
		t.Fatalf("output contains replacement char: %q", out)
	}
}
