package setup

import (
	"sort"
	"strings"
	"unicode"
)

// Candidate is a tool of a large catalog (imported skills, thousands of
// them) with the words it is known by: name, category, tags.
type Candidate struct {
	Tool
	Keys []string
}

// maxRanked caps how many catalog tools go into the prompt.
const maxRanked = 40

// Rank keeps the catalog tools that match the project (its scan and goal),
// best first, at most maxRanked: a word of the project found in a tool's
// name, category or tags counts 3, in its description 1, a word common to
// much of the catalog nothing. Tools scoring under 3 are left out.
func Rank(hint string, cands []Candidate) []Tool {
	words := hintWords(hint)
	if len(words) == 0 {
		return nil
	}
	type doc struct {
		t          Tool
		keys, desc map[string]bool
	}
	docs := make([]doc, len(cands))
	df := map[string]int{} // how many tools a project word appears in
	for i, c := range cands {
		d := doc{t: c.Tool, keys: map[string]bool{}, desc: map[string]bool{}}
		for _, k := range append([]string{c.Name}, c.Keys...) {
			for _, w := range splitWords(k) {
				d.keys[w] = true
			}
		}
		for _, w := range splitWords(c.Description) {
			d.desc[w] = true
		}
		for w := range words {
			if d.keys[w] || d.desc[w] {
				df[w]++
			}
		}
		docs[i] = d
	}
	// a word found in many tools ("agent", "web") says little: it is left
	// out past 2% of the catalog (at least 5 tools)
	common := max(5, len(cands)/50)
	type scored struct {
		t Tool
		n int
	}
	var out []scored
	for _, d := range docs {
		n := 0
		for w := range words {
			switch {
			case df[w] > common:
			case d.keys[w]:
				n += 3
			case d.desc[w]:
				n++
			}
		}
		if n >= 3 {
			out = append(out, scored{d.t, n})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].n > out[j].n })
	if len(out) > maxRanked {
		out = out[:maxRanked]
	}
	tools := make([]Tool, len(out))
	for i, s := range out {
		tools[i] = s.t
	}
	return tools
}

// stopWords are words of a scan or goal that say nothing about the stack.
var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "project": true, "file": true, "files": true,
	"src": true, "app": true, "lib": true, "test": true, "tests": true, "main": true, "json": true,
	"md": true, "readme": true, "yes": true, "none": true, "dir": true, "use": true, "using": true,
	"của": true, "và": true, "cho": true, "các": true, "một": true, "những": true, "này": true,
}

func hintWords(hint string) map[string]bool {
	out := map[string]bool{}
	for _, w := range splitWords(hint) {
		if len([]rune(w)) >= 2 && !stopWords[w] {
			out[w] = true
		}
	}
	return out
}

// splitWords lowercases s and splits it on anything but letters and digits
// ("nuxt-seo" → nuxt, seo).
func splitWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}
