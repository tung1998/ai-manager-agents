package attach

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00")

func TestSaveResolve(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	img, err := s.Save("p1", "a@x", "../../shot.png", png)
	if err != nil || img.Kind != "image" || img.Name != "shot.png" {
		t.Fatal(img, err)
	}
	doc, err := s.Save("p1", "a@x", "notes.md", []byte("# hi"))
	if err != nil || doc.Kind != "text" {
		t.Fatal(doc, err)
	}
	pdf, _ := s.Save("p1", "a@x", "a.pdf", []byte("%PDF-1.4\n..."))
	if pdf.Kind != "pdf" {
		t.Fatal(pdf)
	}
	if _, err := s.Save("p1", "a@x", "a.bin", []byte{0, 1, 2, 3}); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	files, err := s.Resolve("p1", []string{img.ID, doc.ID})
	if err != nil || len(files) != 2 || files[1].Text != "# hi" {
		t.Fatal(files, err)
	}
	if _, err := s.Resolve("p2", []string{img.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatal("other project must not use it")
	}
	if _, _, err := s.Get("../etc"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	p := InlineText("Q", files, func(f File) bool { return f.Kind == "image" })
	if !strings.Contains(p, `<attachment name="notes.md">`) || !strings.Contains(p, "shot.png") {
		t.Fatal(p)
	}
}

func TestSafeNameUTF8Truncation(t *testing.T) {
	// Place "ă" (2-byte UTF-8) straddling byte offset 116 (120-len(".txt")),
	// so a byte-based cut slices the rune in half; a rune-based cut must not.
	name := strings.Repeat("a", 115) + "ă" + strings.Repeat("x", 10) + ".txt"
	got := SafeName(name)
	if !utf8.ValidString(got) {
		t.Fatalf("SafeName produced invalid UTF-8: %q", got)
	}
	if strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("SafeName produced replacement char: %q", got)
	}
	if len(got) > 120 {
		t.Fatalf("SafeName exceeded 120 bytes: %d bytes: %q", len(got), got)
	}
}

func TestSafeNameLongVietnameseStaysWithinByteLimit(t *testing.T) {
	// 150 copies of a 3-byte rune: the old rune-count limit (120 runes)
	// would keep all 450 bytes, exceeding filesystem name limits.
	name := strings.Repeat("ệ", 150) + ".txt"
	got := SafeName(name)
	if !utf8.ValidString(got) {
		t.Fatalf("SafeName produced invalid UTF-8: %q", got)
	}
	if len(got) > 120 {
		t.Fatalf("SafeName exceeded 120 bytes: %d bytes: %q", len(got), got)
	}
	if !strings.HasSuffix(got, ".txt") {
		t.Fatalf("SafeName dropped the extension: %q", got)
	}
}

func TestSafeNameLongExtensionDoesNotPanic(t *testing.T) {
	// An extension at or past the 120-byte limit must not make the cut
	// index negative.
	name := "x." + strings.Repeat("a", 130)
	got := SafeName(name)
	if !utf8.ValidString(got) {
		t.Fatalf("SafeName produced invalid UTF-8: %q", got)
	}
	if len(got) > 120 {
		t.Fatalf("SafeName exceeded 120 bytes: %d bytes: %q", len(got), got)
	}
}
