---
key: content-review
name: Review nội dung
category: Content
description: Kiểm tra nội dung về accuracy, giọng điệu, khả năng đọc, accessibility và rủi ro claim.
input: Bản nháp cần review
inputs:
  - { key: context, description: "Draft, audience, channel, brand guide và nguồn tham chiếu", required: true }
outputs:
  - { key: findings, description: "Lỗi theo mức độ, đoạn liên quan và đề xuất sửa", required: true, type: list }
  - { key: verdict, description: "ready | revise | blocked", required: true }
roles:
  - { key: editor, name: Biên tập, hint: clarity, structure, tone }
  - { key: factchecker, name: Kiểm chứng, hint: claims, sources, dates, differ_from: [editor] }
  - { key: accessibility, name: Accessibility, hint: readability, inclusive language, alt text }
parallel: [[editor, factchecker], [accessibility]]
limits: { rounds: 2, turns: 8, timeout: 2h }
brief: [context]
---
1. Ghi finding cụ thể theo đoạn, không viết lại toàn bài nếu chưa cần.
2. Kiểm tra claim bằng nguồn được cung cấp, đánh dấu điều chưa xác minh.
3. Gọi `workflow_done` với findings và verdict.
