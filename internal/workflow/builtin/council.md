---
key: council
name: Hội đồng
description: Hai agent khác hãng model phân tích độc lập rồi thống nhất một kế hoạch. Dùng khi bế tắc, sửa mãi không xong, hay bài toán khó cần lùi lại tìm nguyên nhân gốc.
input: Vấn đề cần phân tích
inputs:
  - { key: question, description: "Vấn đề cần phân tích (điều gì sai, kết quả cần đạt)", required: true }
  - { key: context, description: "Đã thử gì, vì sao chưa được" }
outputs:
  - { key: plan, description: "Kế hoạch hai bên thống nhất", required: true }
  - { key: agreed, description: "Hai bên có thống nhất không", type: boolean, required: true }
roles:
  - key: a
    name: Thành viên A
    hint: lập kế hoạch, phân tích nguyên nhân gốc
    access: analyze
    prefer: { tier: strong }
  - key: b
    name: Thành viên B
    hint: phản biện, tìm rủi ro
    access: analyze
    differ_from: [a]
    prefer: { tier: strong }
parallel: [[a, b]]
limits: { rounds: 3, turns: 8, timeout: 1h }
brief: [question, context]
---
1. Viết đề bài ở mức vấn đề (`question`): điều gì đang sai, kết quả cần đạt. Không đề xuất cách làm. `context` gồm những gì đã thử và vì sao chưa được (lấy từ bối cảnh cuộc chat đã gọi); `files` là đường dẫn.
2. Gọi `workflow_delegate` cho cả `a` và `b` trong cùng một lượt, cùng một đề bài. Ghi ngắn rồi dừng lượt.
3. Có kết quả của cả hai: so sánh. Chỗ bất đồng thì gửi lập luận của bên này cho bên kia bằng `workflow_send` và hỏi họ có đổi ý không.
4. Dừng khi hai bên thống nhất hoặc hết số vòng. Gọi `workflow_done`: `summary` là kết luận chung, kế hoạch đề xuất, chỗ từng bất đồng và cách giải quyết; `outputs.plan`, `outputs.agreed`.
