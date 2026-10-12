package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ripgrep finds what the walk in Go finds: the same folders, hidden ones and
// secrets skipped, .gitignore not followed, big and binary files left out,
// the glob and the 200 cap honoured.
func TestSearchRipgrepSameAsWalk(t *testing.T) {
	if ripgrepPath() == "" {
		t.Skip("no ripgrep on this machine")
	}
	root := t.TempDir()
	put := func(rel, body string) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("src/a.go", "package a\n// Needle here\nfunc x() {} // needle again\n")
	put("src/ignored.txt", "needle in an ignored file\n")
	put(".gitignore", "src/ignored.txt\n")
	put(".eslintrc", "needle in a hidden file\n")
	put(".hid/b.txt", "needle in a hidden folder\n")
	put("node_modules/x/c.js", "needle\n")
	put("build/out.txt", "needle\n")
	put(".env", "NEEDLE=1\n")
	put("keys/server.PEM", "needle\n")
	put("big.txt", strings.Repeat("needle\n", 400_000))
	put("bin.dat", "needle\x00\x01")
	put("many.md", strings.Repeat("many haystack\n", 250))
	w := Workspace{Root: root}
	for _, c := range []struct{ pattern, glob string }{{"needle", ""}, {"needle", "*.go"}, {"nEEdle\\s+here", ""}, {"many haystack", ""}, {"nothing-like-this", ""}} {
		rg, err := w.Search(c.pattern, c.glob)
		if err != nil {
			t.Fatal(err)
		}
		save := rgPath
		rgPath = ""
		walk, _ := w.Search(c.pattern, c.glob)
		rgPath = save
		if rg != walk {
			t.Errorf("%q %q:\nrg:\n%s\nwalk:\n%s", c.pattern, c.glob, rg, walk)
		}
	}
	if out, _ := w.Search("needle", ""); !strings.Contains(out, "src/ignored.txt:1:") || strings.Contains(out, "node_modules") || strings.Contains(out, ".env") || strings.Contains(out, "PEM") {
		t.Errorf("found: %s", out)
	}
}
