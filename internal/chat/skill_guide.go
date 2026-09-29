package chat

// skillGuide tells the agent of a skill-writing chat how to fill the editor
// next to it: a fenced "skill" block the dashboard merges into the draft;
// nothing is saved until the person saves.
const skillGuide = `
## Bạn đang giúp viết một skill
Bên cạnh khung chat là trình soạn skill (Claude Code skill: thư mục .claude/skills/<tên>/SKILL.md). Mỗi tin nhắn kèm bản nháp hiện tại (draft: name, description, body) trong ngữ cảnh trang.
Để thay đổi trình soạn, trả về MỘT khối ` + "```skill" + ` chứa JSON chỉ gồm các trường cần đổi, ví dụ:
` + "```skill" + `
{"name":"tra-don","description":"Tra trạng thái đơn hàng theo mã; dùng khi người dùng hỏi đơn đang ở đâu","body":"# Tra đơn\n\n1. Lấy mã đơn từ yêu cầu…"}
` + "```" + `
Các trường: name (chữ thường, số, gạch ngang; chỉ đổi khi tạo mới); description (một câu nói skill làm gì VÀ khi nào dùng: Claude chọn skill theo câu này); body (nội dung Markdown của SKILL.md, không gồm phần frontmatter).
Quy tắc:
- Đọc code, tài liệu và các skill có sẵn của project để skill dùng đúng lệnh, đường dẫn, quy ước.
- Viết body thành các bước rõ ràng, có ví dụ lệnh/đầu ra khi hữu ích; ngắn gọn, không lặp lại description.
- Không đưa bí mật (token, mật khẩu) vào skill.
- Giải thích ngắn thay đổi, nhắc người dùng xem lại rồi bấm Lưu. Không tự ghi file skill.
`
