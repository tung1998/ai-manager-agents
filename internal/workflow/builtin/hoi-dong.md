---
key: hoi-dong
name: Hội đồng
description: Hai agent khác hãng model phân tích độc lập rồi thống nhất một kế hoạch. Dùng khi bế tắc, sửa mãi không xong, hay bài toán khó cần lùi lại tìm nguyên nhân gốc.
input: Vấn đề cần phân tích
roles:
  - key: a
    name: Thành viên A
    hint: lập kế hoạch, phân tích nguyên nhân gốc
    access: analyze
  - key: b
    name: Thành viên B
    hint: phản biện, tìm rủi ro
    access: analyze
    differ_from: [a]
parallel: [[a, b]]
limits: { rounds: 3, turns: 8, timeout: 1h }
brief: [question, context]
---
1. Viết đề bài ở mức vấn đề (`question`): điều gì đang sai, kết quả cần đạt. Không đề xuất cách làm. `context` gồm những gì đã thử và vì sao chưa được; `files` là đường dẫn.
2. Gọi `workflow_delegate` cho cả `a` và `b` trong cùng một lượt, cùng một đề bài. Báo người dùng hội đồng đang phân tích rồi dừng lượt.
3. Có kết quả của cả hai: so sánh. Chỗ bất đồng thì gửi lập luận của bên này cho bên kia bằng `workflow_send` và hỏi họ có đổi ý không.
4. Dừng khi hai bên thống nhất hoặc hết số vòng. Báo người dùng: kết luận chung, kế hoạch đề xuất, chỗ từng bất đồng và cách giải quyết. Gọi `workflow_done`.
