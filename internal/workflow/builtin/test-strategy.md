---
key: test-strategy
name: Lập chiến lược kiểm thử
category: Engineering
description: Chuyển yêu cầu và rủi ro thành chiến lược test có phạm vi, mức ưu tiên và tiêu chí phát hành.
input: Tính năng hoặc thay đổi cần lập test plan
inputs:
  - { key: context, description: "Requirement, architecture, user flows và rủi ro đã biết", required: true }
  - { key: done_when, description: "Tiêu chí hoàn thành" }
outputs:
  - { key: plan, description: "Test matrix, môi trường, data và owner", required: true, type: list }
  - { key: gaps, description: "Coverage gap và rủi ro còn lại", required: true, type: list }
roles:
  - { key: quality, name: QA, hint: "scenarios, edge cases, regression" }
  - { key: engineering, name: Kỹ thuật, hint: "testability, observability, failure modes", differ_from: [quality] }
  - { key: product, name: Sản phẩm, hint: "acceptance, user impact" }
parallel: [[quality, engineering], [product]]
limits: { rounds: 2, turns: 8, timeout: 2h }
brief: [done_when, context]
---
1. Xác định critical path, abuse case, regression surface và mức confidence cần có.
2. Lập matrix test theo tầng, data, môi trường, owner và lệnh kiểm tra.
3. Gọi `workflow_done` với plan, gaps và tiêu chí go/no-go.
