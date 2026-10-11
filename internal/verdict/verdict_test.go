package verdict

import "testing"

func TestOf(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"VERDICT: AGREE\nok", "yes"},
		{"VERDICT: DISAGREE\nno", "no"},
		{"analysis\n**Kết luận: AGREE.** Bản vá đúng. Ý kiến B (DISAGREE) không đứng được.", "yes"},
		{"VERDICT: AGREE with A, B's DISAGREE is wrong", "yes"},
		{"KẾT LUẬN: KHÔNG ĐỒNG Ý", "no"},
		{"KẾT LUẬN: ĐỒNG Ý", "yes"},
		{"**VERDICT:** DISAGREE", "no"},
		{"Kết luận: lỗi không xảy ra (AGREE với B)", "unclear"},
		{"I AGREE", "unclear"},
	} {
		if got := Of(c.in); got != c.want {
			t.Errorf("Of(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}
