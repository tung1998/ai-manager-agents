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
steps:
  - id: review
    type: coordinate
    roles: [a, b]
    prompt: |
      1. Giao cùng lúc a và b review {{input.text}}, mỗi bên một bản bàn giao đủ bối cảnh.
      2. Chỗ hai bên khác nhau thì hỏi lại đúng bên đó bằng workflow_send.
      3. Gọi workflow_done với kết luận.
    next: xong
  - id: xong
    type: end
    summary: "{{steps.review.output}}"
---
Quy trình chạy theo các bước ở phần đầu (steps).
` + "```" + `
Phần đầu (office ép đúng các luật này khi chạy):
- key (chữ thường, số, gạch ngang; là lệnh #key trong chat), name, description (quy trình dùng khi nào), input (người dùng cần đưa gì).
- roles: key, name, hint (chuyên môn cần, để gợi ý agent), access: analyze (chỉ đọc) | propose (đề xuất, người duyệt) | edit (sửa trong worktree), differ_from [vai phải dùng model khác hãng; "dieu-phoi" là agent điều phối].
- Vai là quy trình con: thêm workflow: <key quy trình khác của project> (kể cả chính nó: đệ quy). Giao việc cho vai đó là chạy quy trình con trong chat riêng, bản giao là đầu vào, đầu ra của nó là câu trả lời của vai; agent gán cho vai điều phối nó (không gán: agent điều phối hiện tại); access của vai là trần quyền cho cả quy trình con (mặc định edit = không hạ).
- inputs / outputs: danh sách { key, description, required, type } (type của output: string | number | boolean | list | json; sai kiểu thì workflow_done bị từ chối). inputs là những gì người gọi đưa vào (quy trình cha giao theo đúng key); outputs là kết quả có tên, workflow_done phải có đủ các key required.
- callable: chat (chỉ gõ #key trong chat) | sub (chỉ quy trình khác gọi, ẩn khỏi gợi ý /) | bỏ trống = cả hai.
- prefer trong vai: { tier: strong|balanced|fast, family: anthropic|openai|google|… } để office gợi ý agent khi cài.
- parallel: nhóm vai được chạy cùng lúc; limits: rounds (số lần hỏi lại một vai), turns (tổng lượt), timeout, budget_usd, depth (số cấp quy trình con được lồng bên dưới, mặc định 2, tối đa 5), concurrency (số vai làm cùng lúc trong cả cây quy trình con, mặc định 6), idle (vai làm quá lâu thì báo, ví dụ 15m); budget_usd tính cả chi phí của quy trình con.
- brief: mục bắt buộc của bản bàn giao (outcome, question, context, constraints, current_option, tried, files, done_when, must_not).
- gates (cổng phải qua trước khi xong): key, name, kind: approve (người duyệt) | check (lệnh kiểm tra của project), required.
- vote (biểu quyết): roles, quorum, veto [vai có quyền phủ quyết].
- supervise (giám sát): { role: <vai analyze>, every: 10m (ít nhất 2m) }. Office tự gọi vai đó định kỳ khi các vai đang làm để đối chiếu hướng đi với yêu cầu gốc; thấy lệch thì báo điều phối ở lần gọi lại. Không giao việc được cho vai giám sát. Mọi vai đều được mở đầu câu trả lời bằng PHẢN BIỆN khi thấy bản giao sai; điều phối phải trả lời (workflow_send) hoặc ghi overrule ở workflow_done.
- strict: true thì từ chối thay vì cảnh báo khi differ_from không đạt.
- steps (quy trình là các bước office chạy theo dây nối): danh sách { id, type, name, next, on_error: stop|continue }; id chữ thường, số, gạch ngang hoặc gạch dưới. Bắt đầu ở start: [id, …] (bỏ trống = bước đầu danh sách). next, then, else nhận một id hoặc danh sách [a, b]: các bước đó chạy song song; bước có nhiều nhánh đi vào chờ đủ các nhánh rồi chạy một lần (đọc output của từng nhánh). on_error: continue thì lỗi cũng là output (status "error") cho bước sau. type:
  agent { role, prompt } (role trong roles, project gán agent; một lượt trả lời) · coordinate { roles: [vai được giao việc, bỏ trống = mọi vai], role (vai làm điều phối, bỏ trống = agent đang chạy quy trình), prompt (hướng dẫn cho agent điều phối) } (agent điều phối tự giao việc, hỏi lại, biểu quyết, qua cổng bằng các công cụ workflow_*; workflow_done là output của bước, outputs đọc bằng {{steps.<id>.json.key}}) · workflow { workflow: key, inputs: {key: template} } · code { lang: bash|node|python, script, timeout_s } (nhận dữ liệu JSON qua stdin và biến OFFICE_INPUT_*; stdout là output) · http { method, url, headers, body } (body trả về là output) · condition { if, then, else, max_loops } (if: "A == B", !=, >, <, >=, <=, contains; quay lại bước trước là lặp) · switch { value: template, cases: [{ when, next }], else } (value bằng when nào, không phân biệt hoa thường, thì đi nhánh đó; không khớp thì else; status là value) · approve { note, else } (người dùng duyệt) · check { command, else } · end { summary, outputs: {key: template} }.
  Template: {{input.key}}, {{steps.<id>.output}}, {{steps.<id>.json.a.b}}, {{steps.<id>.status}}. Thứ tự cố định thì nối các bước; đoạn nào cần AI tự quyết (giao việc, hỏi lại, nhiều vòng) thì dùng một bước coordinate. parallel, limits, brief, gates, vote ở phần đầu áp dụng cho các bước coordinate.
Prompt của bước coordinate (hoặc phần thân ở file cũ không có steps, chạy như một bước coordinate với mọi vai): các bước cho agent điều phối, dùng công cụ workflow_delegate, workflow_send, workflow_ask (hỏi vai analyze và chờ trả lời ngay), workflow_vote, workflow_gate, workflow_done. Nêu kết quả cần đạt và điểm dừng, không viết sẵn cách sửa từng file.
Quy tắc: quyền thấp nhất đủ dùng cho từng vai; vai kiểm tra hay phản biện nên differ_from vai làm; nếu error trong ngữ cảnh còn lỗi thì sửa cho hết. Giải thích ngắn thay đổi, nhắc người dùng xem lại rồi bấm Lưu. Không tự lưu.`
