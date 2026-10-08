package burn

import (
	"fmt"
	"strings"
)

// The person's focus steers a Burn (ADR-120): a direction, not a limit — it
// still does all its work; what fits the focus is looked at and picked first,
// and done and checked its way.
// Some focuses come with what to look at (a lens), found by their words.

type lens struct {
	words []string // in the focus, lower case
	look  string   // what to look for (scan)
	check string   // how a piece is checked (work)
}

var lenses = []lens{
	{[]string{"bảo mật", "security", "an toàn", "secure", "lỗ hổng", "auth"},
		"authorization and authentication on every API/route, injection (SQL, shell commands, paths), XSS, CSRF, SSRF, leaked secrets (logs, errors, responses), how tokens/passwords are encrypted and stored, rate limits, file uploads, vulnerable dependencies",
		"add tests for the attack / no-permission cases, and leak nothing more in errors or logs"},
	{[]string{"ui", "ux", "giao diện", "trải nghiệm", "frontend", "design"},
		"whether the main flows are short, loading/error/empty states, feedback after an action, mobile and narrow screens, unclear or untranslated text, consistency across pages, keyboard access and contrast",
		"run the UI typecheck/lint, check narrow screens and the loading/error/empty states, change no behaviour out of scope"},
	{[]string{"hiệu năng", "performance", "tốc độ", "nhanh", "chậm", "tối ưu"},
		"N+1 queries and missing indexes, wasted loops or I/O, goroutine/memory leaks, large payloads, needless UI re-renders, caching",
		"measure before and after (benchmark, timings, query counts) and put the numbers in the summary"},
	{[]string{"test", "kiểm thử", "coverage"},
		"key paths without tests, flaky tests, untested edge cases",
		"a new test must fail without the fix and run reliably"},
	{[]string{"tài liệu", "docs", "document"},
		"docs out of step with the code, guides missing steps, undocumented APIs/settings",
		"check each sentence against the real code"},
}

// focusLenses are the lenses the focus asks for.
func focusLenses(focus string) []lens {
	f := " " + strings.ToLower(focus) + " "
	var out []lens
	for _, l := range lenses {
		for _, w := range l.words {
			// a short word (ui, ux, auth) only as a word of its own
			if len(w) <= 4 && !strings.ContainsAny(w, " ") {
				if strings.Contains(strings.NewReplacer(",", " ", ".", " ", "/", " ", ";", " ", "(", " ", ")", " ").Replace(f), " "+w+" ") {
					out = append(out, l)
					break
				}
				continue
			}
			if strings.Contains(f, w) {
				out = append(out, l)
				break
			}
		}
	}
	return out
}

// focusPlan is the focus as a scan reads it: above the order of work.
func focusPlan(focus string) string {
	if focus == "" {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "\nThe person's focus (a direction, not a limit):\n%s\n", focus)
	sb.WriteString("- Burn still scans and does all kinds of work as usual; the focus only decides what is looked at and picked first.\n")
	sb.WriteString("- When scanning, look at the areas related to the focus first (read it broadly), then the others.\n")
	sb.WriteString("- When picking, pieces serving the focus go first; with none, pick other pieces as usual. Never burn_skip a piece only because it is off the focus.\n")
	for _, l := range focusLenses(focus) {
		fmt.Fprintf(&sb, "- For this focus, look at: %s.\n", l.look)
	}
	return sb.String()
}

// focusWork is the focus as a piece's work reads it.
func focusWork(focus string) string {
	if focus == "" {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "The person's focus (lean towards it when there is a choice of how): %s\n", focus)
	for _, l := range focusLenses(focus) {
		fmt.Fprintf(&sb, "Verify for the focus: %s.\n", l.check)
	}
	return sb.String()
}

// focusReview is the focus as a reviewer reads it at stage.
func focusReview(focus, stage string) string {
	if focus == "" {
		return ""
	}
	s := fmt.Sprintf("The person's focus (a direction, not a reason to turn other work down): %s\n", focus)
	switch stage {
	case "result":
		for _, l := range focusLenses(focus) {
			s += "Check the result for the focus: " + l.check + ".\n"
		}
	}
	return s
}
