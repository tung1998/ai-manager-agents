package chat

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// A cut inside a multi-byte rune used to hand codex invalid UTF-8 on stdin,
// failing every review step of the Burn.
func TestTruncateKeepsValidUTF8(t *testing.T) {
	s := strings.Repeat("Góc nhìn ", 50)
	for n := 1; n < len(s); n++ {
		if got := truncate(s, n); !utf8.ValidString(got) {
			t.Fatalf("truncate(%d) gave invalid UTF-8: %q", n, got)
		}
	}
	if got := truncate("abc", 5); got != "abc" {
		t.Fatalf("short string changed: %q", got)
	}
}
