---
key: vendor-onboarding
name: Đánh giá và onboarding nhà cung cấp
category: Operations
description: Kiểm tra năng lực, rủi ro, tài liệu và kế hoạch tích hợp trước khi dùng nhà cung cấp mới.
input: Nhà cung cấp hoặc đối tác cần onboarding
inputs:
  - { key: context, description: "Dịch vụ, data access, SLA, giá và hồ sơ nhà cung cấp", required: true }
outputs:
  - { key: assessment, description: "Đánh giá theo checklist và evidence", required: true, type: list }
  - { key: plan, description: "Kế hoạch onboarding và owner", required: true, type: list }
roles:
  - { key: procurement, name: Mua sắm, hint: "commercial, references, terms" }
  - { key: security, name: Bảo mật, hint: "data, access, controls", differ_from: [procurement] }
  - { key: operations, name: Vận hành, hint: "SLA, support, integration, exit plan" }
parallel: [[procurement, security], [operations]]
gates:
  - { key: approve, name: Duyệt nhà cung cấp, kind: approve, required: true }
limits: { rounds: 2, turns: 9, timeout: 3h }
brief: [context]
---
1. Kiểm tra hồ sơ, dữ liệu truy cập, SLA, subprocessor, exit plan và bằng chứng kiểm soát.
2. Ghi rõ gap, owner, deadline và risk acceptance nếu có.
3. Mở gate `approve`; sau khi duyệt gọi `workflow_done` với assessment và plan.
