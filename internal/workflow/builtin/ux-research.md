---
key: ux-research
name: Nghiên cứu trải nghiệm người dùng
category: Product
description: Tổng hợp phỏng vấn, hành vi và phản hồi thành insight có thể kiểm chứng.
input: Câu hỏi nghiên cứu hoặc vấn đề UX
inputs:
  - { key: context, description: "Audience, transcript, analytics, prototype và câu hỏi", required: true }
outputs:
  - { key: insights, description: "Insight kèm evidence và confidence", required: true, type: list }
  - { key: recommendations, description: "Khuyến nghị và câu hỏi cần nghiên cứu thêm", required: true, type: list }
roles:
  - { key: qualitative, name: Định tính, hint: "interviews, themes, quotes" }
  - { key: quantitative, name: Định lượng, hint: "analytics, funnel, segments", differ_from: [qualitative] }
  - { key: design, name: Thiết kế, hint: "usability, prototype, opportunity" }
parallel: [[qualitative, quantitative], [design]]
limits: { rounds: 2, turns: 8, timeout: 3h }
brief: [context]
---
1. Tách observation, interpretation và assumption; không khái quát quá mẫu.
2. Đối chiếu tín hiệu định tính với dữ liệu hành vi và tìm mâu thuẫn.
3. Gọi `workflow_done` với insights và recommendations.
