---
key: roadmap-prioritization
name: Ưu tiên roadmap
category: Product
description: Xếp hạng cơ hội roadmap theo tác động khách hàng, chiến lược, effort và bằng chứng.
input: Danh sách cơ hội hoặc backlog cần ưu tiên
inputs:
  - { key: context, description: "Ideas, feedback, metrics, constraints và mục tiêu quý", required: true }
  - { key: outcome, description: "Mục tiêu sản phẩm cần tối ưu" }
outputs:
  - { key: ranking, description: "Bảng ưu tiên và lý do", required: true, type: list }
  - { key: experiments, description: "Thử nghiệm nhỏ để giảm bất định", type: list }
roles:
  - { key: customer, name: Khách hàng, hint: "pain, segment, demand evidence" }
  - { key: business, name: Kinh doanh, hint: "strategy, revenue, positioning", differ_from: [customer] }
  - { key: delivery, name: Triển khai, hint: "effort, dependencies, risks" }
parallel: [[customer, business], [delivery]]
limits: { rounds: 3, turns: 9, timeout: 3h }
brief: [outcome, context]
---
1. Chuẩn hóa cơ hội thành problem statement, segment, evidence và confidence.
2. So sánh impact, urgency, effort, dependency và chi phí cơ hội; nêu cả phương án không làm.
3. Gọi `workflow_done` với ranking và experiments.
