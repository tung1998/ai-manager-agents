---
key: performance-review
name: Chuẩn bị đánh giá hiệu suất
category: People
description: Tổng hợp mục tiêu, kết quả và phản hồi thành bản đánh giá công bằng, có ví dụ và mục tiêu tiếp theo.
input: Kỳ đánh giá và phạm vi nhân sự
inputs:
  - { key: context, description: "Goals, outcomes, feedback và level expectations", required: true }
outputs:
  - { key: review, description: "Tóm tắt thành tựu, gap và evidence", required: true }
  - { key: goals, description: "Mục tiêu phát triển kỳ tới", required: true, type: list }
roles:
  - key: manager
    name: Quản lý
    hint: outcomes, expectations, coaching
  - key: peer
    name: Đồng nghiệp
    hint: collaboration, evidence, impact
    differ_from: [manager]
  - key: people
    name: People ops
    hint: rubric, consistency, process
parallel: [[manager, peer], [people]]
limits: { rounds: 2, turns: 8, timeout: 2h }
brief: [context]
---
1. Chỉ dùng evidence liên quan đến mục tiêu và hành vi công việc; loại bỏ suy đoán về đặc điểm nhạy cảm.
2. Kiểm tra consistency với rubric và level, phân biệt impact với activity.
3. Gọi `workflow_done` với review và goals.
