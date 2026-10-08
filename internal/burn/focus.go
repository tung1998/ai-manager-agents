package burn

import "strings"

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

// focusLooks is what the focus asks a scan to look at (burn/plan.md).
func focusLooks(focus string) []string {
	var out []string
	for _, l := range focusLenses(focus) {
		out = append(out, l.look)
	}
	return out
}

// focusChecks is how the focus asks a piece to be checked (burn/work.md,
// burn/review.md at the result).
func focusChecks(focus string) []string {
	var out []string
	for _, l := range focusLenses(focus) {
		out = append(out, l.check)
	}
	return out
}
