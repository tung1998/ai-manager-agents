package burn

import (
	"fmt"
	"strings"
)

// The person's focus steers every step of a Burn (ADR-120): what a scan looks
// for and picks, how a piece is done and checked, what a reviewer accepts.
// Some focuses come with what to look at (a lens), found by their words.

type lens struct {
	words []string // in the focus, lower case
	look  string   // what to look for (scan)
	check string   // how a piece is checked (work)
}

var lenses = []lens{
	{[]string{"bảo mật", "security", "an toàn", "secure", "lỗ hổng", "auth"},
		"kiểm tra quyền và xác thực ở mọi API/route, injection (SQL, lệnh shell, đường dẫn), XSS, CSRF, SSRF, lộ bí mật (log, lỗi, response), mã hóa và lưu token/mật khẩu, giới hạn tốc độ, file upload, phụ thuộc có lỗ hổng",
		"thêm test cho trường hợp tấn công/không có quyền, không làm lộ thêm thông tin trong lỗi hay log"},
	{[]string{"ui", "ux", "giao diện", "trải nghiệm", "frontend", "design"},
		"luồng thao tác chính có gọn không, trạng thái đang tải/lỗi/rỗng, phản hồi sau thao tác, mobile và màn hẹp, chữ khó hiểu hoặc chưa dịch, nhất quán giữa các trang, truy cập bằng bàn phím và độ tương phản",
		"chạy typecheck/lint của giao diện, xem lại trên màn hẹp và các trạng thái tải/lỗi/rỗng, không đổi hành vi ngoài phạm vi"},
	{[]string{"hiệu năng", "performance", "tốc độ", "nhanh", "chậm", "tối ưu"},
		"truy vấn N+1 và thiếu index, vòng lặp hay I/O thừa, rò rỉ goroutine/bộ nhớ, payload lớn, render lại thừa ở giao diện, cache",
		"đo trước và sau (benchmark, thời gian, số truy vấn) và ghi số đo vào tóm tắt"},
	{[]string{"test", "kiểm thử", "coverage"},
		"đường quan trọng chưa có test, test chập chờn, trường hợp biên chưa được kiểm",
		"test mới phải fail khi bỏ phần sửa và chạy ổn định"},
	{[]string{"tài liệu", "docs", "document"},
		"tài liệu lệch với code, hướng dẫn thiếu bước, API/cấu hình chưa được mô tả",
		"đối chiếu từng câu với code thật"},
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
	fmt.Fprintf(&sb, "\nTRỌNG TÂM CỦA NGƯỜI DÙNG (ưu tiên trên thứ tự bên dưới):\n%s\n", focus)
	sb.WriteString("- Hiểu trọng tâm theo nghĩa rộng của người dùng: trước tiên liệt kê các vùng code/tài liệu liên quan tới nó (đọc cấu trúc project), rồi quét lần lượt từng vùng, không dừng ở một file.\n")
	sb.WriteString("- Ưu tiên ghi và chọn việc phục vụ trọng tâm. Việc ngoài trọng tâm chỉ ghi khi nghiêm trọng (bảo mật, mất dữ liệu, hỏng chức năng chính).\n")
	sb.WriteString("- Việc đã có mà nằm ngoài trọng tâm thì ĐỂ NGUYÊN (không burn_skip vì lý do trọng tâm: skip là bỏ hẳn). Hết việc trong trọng tâm thì chọn chúng.\n")
	sb.WriteString("- Trong chi tiết mỗi việc, ghi rõ nó phục vụ trọng tâm thế nào.\n")
	for _, l := range focusLenses(focus) {
		fmt.Fprintf(&sb, "- Với trọng tâm này hãy xem: %s.\n", l.look)
	}
	return sb.String()
}

// focusWork is the focus as a piece's work reads it.
func focusWork(focus string) string {
	if focus == "" {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Trọng tâm người dùng dặn (theo nó khi có lựa chọn cách làm): %s\n", focus)
	for _, l := range focusLenses(focus) {
		fmt.Fprintf(&sb, "Kiểm chứng theo trọng tâm: %s.\n", l.check)
	}
	return sb.String()
}

// focusReview is the focus as a reviewer reads it at stage.
func focusReview(focus, stage string) string {
	if focus == "" {
		return ""
	}
	s := fmt.Sprintf("Trọng tâm người dùng dặn: %s\n", focus)
	switch stage {
	case "issue":
		s += "Việc không phục vụ trọng tâm thì KHÔNG ĐỒNG Ý, trừ khi là lỗi nghiêm trọng (bảo mật, mất dữ liệu, hỏng chức năng chính).\n"
	case "result":
		for _, l := range focusLenses(focus) {
			s += "Xem kết quả có đạt theo trọng tâm: " + l.check + ".\n"
		}
	}
	return s
}
