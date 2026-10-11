// Package verdict reads a reviewer's or a judge's conclusion from what it
// answered: a line "VERDICT: AGREE" / "VERDICT: DISAGREE" (older answers:
// "KẾT LUẬN: ĐỒNG Ý" / "KẾT LUẬN: KHÔNG ĐỒNG Ý"). Burn's reviews and the goal
// judge of automations (ADR-132) share it.
package verdict

import "strings"

// The first line asked of an answer (Of reads it).
const (
	Agree    = "VERDICT: AGREE"
	Disagree = "VERDICT: DISAGREE"
)

// Of reads the conclusion: "yes", "no" or "unclear". The prompt asks for a
// first line starting with "VERDICT:", but reasoning models often write
// analysis first and put it last, so only lines starting with that prefix
// count — a bare AGREE/ĐỒNG Ý substring would also fire on quoted prompt text
// or on the answer discussing the format. The first line wins if it matches;
// otherwise the last matching line.
func Of(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if v := lineOf(lines[0]); v != "" {
		return v
	}
	result := "unclear"
	for _, line := range lines[1:] {
		if v := lineOf(line); v != "" {
			result = v
		}
	}
	return result
}

func lineOf(line string) string {
	l := strings.ToUpper(strings.Trim(strings.TrimSpace(line), "*`_# "))
	rest, ok := "", false
	for _, p := range []string{"VERDICT:", "KẾT LUẬN:", "KET LUAN:"} {
		if r, found := strings.CutPrefix(l, p); found {
			rest, ok = strings.TrimLeft(r, " *`_"), true
			break
		}
	}
	if !ok {
		return ""
	}
	// only the word right after the prefix: "VERDICT: AGREE. B's DISAGREE
	// does not hold" is an AGREE
	for _, no := range []string{"DISAGREE", "KHÔNG ĐỒNG Ý", "KHONG DONG Y", "PHẢN ĐỐI"} {
		if strings.HasPrefix(rest, no) {
			return "no"
		}
	}
	for _, yes := range []string{"AGREE", "ĐỒNG Ý", "DONG Y", "TÁN THÀNH"} {
		if strings.HasPrefix(rest, yes) {
			return "yes"
		}
	}
	return ""
}
