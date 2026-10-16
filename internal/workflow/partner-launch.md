---
key: partner-launch
name: Ra mắt với đối tác
category: Sales & Marketing
description: Phối hợp sản phẩm, enablement, thương mại và truyền thông cho một đợt ra mắt qua đối tác.
input: Partner launch hoặc co-marketing campaign
inputs:
  - { key: context, description: "Partner, audience, offer, asset, timeline và dependency", required: true }
  - { key: outcome, description: "Mục tiêu và chỉ số thành công" }
outputs:
  - { key: plan, description: "Launch plan, owner, asset và mốc", required: true, type: list }
  - { key: risks, description: "Risk, dependency và fallback", required: true, type: list }
roles:
  - { key: partner, name: Đối tác, hint: alignment, audience, commitments }
  - { key: marketing, name: Marketing, hint: message, campaign, measurement, differ_from: [partner] }
  - { key: delivery, name: Triển khai, hint: product readiness, support, escalation }
parallel: [[partner, marketing], [delivery]]
gates:
  - { key: launch, name: Duyệt ra mắt, kind: approve, required: true }
limits: { rounds: 2, turns: 9, timeout: 3h }
brief: [outcome, context]
---
1. Chốt audience, message, asset, owner, deadline và điều kiện partner đã xác nhận.
2. Kiểm tra readiness, support, tracking, brand và fallback.
3. Mở gate `launch`; sau khi duyệt gọi `workflow_done` với plan và risks.
