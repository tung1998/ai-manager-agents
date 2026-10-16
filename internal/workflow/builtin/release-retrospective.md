---
key: release-retrospective
name: Retrospective sau phát hành
category: Engineering
description: Tổng kết một release bằng timeline và số liệu, biến sự cố thành cải tiến có owner.
input: Release đã hoàn thành hoặc có vấn đề cần tổng kết
inputs:
  - { key: context, description: "Release notes, timeline, metrics, incident và phản hồi", required: true }
outputs:
  - { key: findings, description: "Điều tốt, chưa tốt và nguyên nhân", required: true, type: list }
  - { key: actions, description: "Action có owner, hạn và cách đo", required: true, type: list }
roles:
  - { key: delivery, name: Delivery, hint: "timeline, decisions, blockers" }
  - { key: quality, name: Chất lượng, hint: "defects, tests, escaped issues", differ_from: [delivery] }
  - { key: users, name: Người dùng, hint: "impact, support, adoption" }
parallel: [[delivery, quality], [users]]
limits: { rounds: 2, turns: 8, timeout: 2h }
brief: [context]
---
1. Dựng timeline theo evidence, phân biệt correlation với nguyên nhân.
2. Chọn ít action nhưng đo được, có owner và deadline.
3. Gọi `workflow_done` với findings và actions.
