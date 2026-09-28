package chat

// automationGuide tells the agent of an automation-building chat how to fill
// the form next to it (ADR-042): a fenced "automation" block the dashboard
// merges into the draft; nothing is saved until the person saves.
const automationGuide = `
## Bạn đang giúp dựng một tự động hóa
Bên cạnh khung chat là form tự động hóa; mỗi tin nhắn kèm bản nháp hiện tại (draft) và lần Chạy thử gần nhất (test) trong ngữ cảnh trang.
Để thay đổi form, trả về MỘT khối ` + "```automation" + ` chứa JSON chỉ gồm các trường cần đổi, ví dụ:
` + "```automation" + `
{"name":"Đếm lỗi log","source":"schedule","config":{"cron":"0 8 * * 1-5","timezone":"Asia/Ho_Chi_Minh"},
 "action":"script","script":{"lang":"bash","body":"grep -c ERROR logs/app.log || true","timeout_s":120},
 "escalate":{"when":"failure","action":"chat","prompt":"Log báo lỗi: {{output}}"}}
` + "```" + `
Các trường: name; source (schedule | webhook); config {every_minutes | cron, timezone, auth, auth_name}; action (script | chat | task);
agent_id; prompt (cho chat/task, có {{payload}}, {{today}}…); script {lang: bash|node|python, body, timeout_s};
escalate {when: never|failure|signal, action: chat|task, agent_id, prompt (có {{output}}, {{exit_code}}, {{message}})};
limits {max_runs_per_hour, daily_cost_usd, disable_after_failures, debounce_seconds, debounce_key, debounce_max_seconds}.
Quy tắc:
- Ưu tiên action=script (không tốn token AI); chỉ gọi agent khi script lỗi hoặc in dòng "@@agent: <nội dung>".
- Đọc code/cấu trúc project trước khi viết script để dùng đúng đường dẫn và lệnh; script chạy trong thư mục project, payload ở stdin và $OFFICE_PAYLOAD.
- Giải thích ngắn gọn thay đổi, nhắc người dùng bấm Chạy thử rồi Lưu. Không tự lưu, không dùng propose_automation trong khung này.
`
