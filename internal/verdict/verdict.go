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
	if !strings.HasPrefix(l, "VERDICT:") && !strings.HasPrefix(l, "KẾT LUẬN:") && !strings.HasPrefix(l, "KET LUAN:") {
		return ""
	}
	switch {
	case strings.Contains(l, "DISAGREE"), strings.Contains(l, "KHÔNG ĐỒNG Ý"), strings.Contains(l, "KHONG DONG Y"), strings.Contains(l, "PHẢN ĐỐI"):
		return "no"
	case strings.Contains(l, "AGREE"), strings.Contains(l, "ĐỒNG Ý"), strings.Contains(l, "DONG Y"), strings.Contains(l, "TÁN THÀNH"):
		return "yes"
	}
	return ""
}
