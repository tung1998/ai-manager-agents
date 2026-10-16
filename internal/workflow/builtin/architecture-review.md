---
key: architecture-review
name: Review kiến trúc
category: Engineering
description: So sánh các phương án kiến trúc theo yêu cầu, rủi ro, chi phí vận hành và khả năng mở rộng.
input: Thay đổi hoặc quyết định kiến trúc cần review
inputs:
  - { key: context, description: "Bối cảnh, constraint, traffic và hệ thống liên quan", required: true }
  - { key: outcome, description: "Kết quả hoặc quyết định cần đạt" }
outputs:
  - { key: recommendation, description: "Phương án đề xuất và lý do", required: true }
  - { key: risks, description: "Rủi ro, trade-off và việc cần kiểm chứng", required: true, type: list }
roles:
  - { key: design, name: Thiết kế, hint: "boundaries, trade-offs, failure modes" }
  - { key: operations, name: Vận hành, hint: "reliability, cost, observability, access", differ_from: [design] }
  - { key: delivery, name: Triển khai, hint: "migration, rollout, rollback" }
parallel: [[design, operations], [delivery]]
limits: { rounds: 2, turns: 8, timeout: 3h }
brief: [outcome, context]
---
1. Giao các vai phân tích độc lập bằng evidence và nêu phương án không làm gì.
2. Hợp nhất trade-off, dependency, migration và rollback; đánh dấu giả định.
3. Gọi `workflow_done` với recommendation, risks và các quyết định cần người phụ trách xác nhận.
