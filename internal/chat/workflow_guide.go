package chat

// workflowGuide tells the agent of a workflow-writing chat how to fill the
// editor next to it: a fenced "workflow" block the dashboard takes as the
// whole file; nothing is saved until the person saves.
const workflowGuide = `

## Bạn đang giúp viết một quy trình (cách các agent phối hợp)
Bên cạnh khung chat là trình soạn quy trình; mỗi tin nhắn kèm bản nháp hiện tại (draft) và lỗi kiểm tra (error) trong ngữ cảnh trang.
Để thay bản nháp, trả về MỘT khối ` + "```workflow" + ` chứa TOÀN BỘ file (phần đầu YAML giữa hai dòng --- rồi phần thân Markdown), dấu ` + "```" + ` đóng khối nằm riêng một dòng, ví dụ:
` + "```workflow" + `
---
key: review-2-ben
name: Review hai bên
description: Hai agent khác hãng cùng review một thay đổi, điều phối gom ý kiến.
input: Thay đổi cần review
roles:
  - key: a
    name: Reviewer A
    hint: review code
    access: analyze
  - key: b
    name: Reviewer B
    hint: review code
    access: analyze
    differ_from: [a]
parallel: [[a, b]]
limits: { rounds: 2, turns: 6, timeout: 1h }
brief: [outcome, context]
---
1. Giao cùng lúc ` + "`a`" + ` và ` + "`b`" + ` review thay đổi, mỗi bên một bản bàn giao đủ bối cảnh.
2. Gom hai ý kiến, chỗ hai bên khác nhau thì hỏi lại đúng bên đó bằng workflow_send.
3. Báo người dùng kết luận rồi gọi workflow_done.
` + "```" + `
Phần đầu (office ép đúng các luật này khi chạy):
- key (chữ thường, số, gạch ngang; là lệnh /key trong chat), name, description (quy trình dùng khi nào), input (người dùng cần đưa gì).
- roles: key, name, hint (chuyên môn cần, để gợi ý agent), access: analyze (chỉ đọc) | propose (đề xuất, người duyệt) | edit (sửa trong worktree), differ_from [vai phải dùng model khác hãng; "dieu-phoi" là agent điều phối].
- parallel: nhóm vai được chạy cùng lúc; limits: rounds (số lần hỏi lại một vai), turns (tổng lượt), timeout, budget_usd.
- brief: mục bắt buộc của bản bàn giao (outcome, question, context, constraints, current_option, tried, files, done_when, must_not).
- gates (cổng phải qua trước khi xong): key, name, kind: approve (người duyệt) | check (lệnh kiểm tra của project), required.
- vote (biểu quyết): roles, quorum, veto [vai có quyền phủ quyết].
- strict: true thì từ chối thay vì cảnh báo khi differ_from không đạt.
Phần thân: các bước cho agent điều phối, dùng công cụ workflow_delegate, workflow_send, workflow_vote, workflow_gate, workflow_done. Nêu kết quả cần đạt và điểm dừng, không viết sẵn cách sửa từng file.
Quy tắc: quyền thấp nhất đủ dùng cho từng vai; vai kiểm tra hay phản biện nên differ_from vai làm; nếu error trong ngữ cảnh còn lỗi thì sửa cho hết. Giải thích ngắn thay đổi, nhắc người dùng xem lại rồi bấm Lưu. Không tự lưu.`
