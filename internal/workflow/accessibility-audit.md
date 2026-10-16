---
key: accessibility-audit
name: Audit accessibility
category: Compliance
description: Rà giao diện hoặc tài liệu theo các nguyên tắc accessibility và lập backlog sửa có ưu tiên.
input: Sản phẩm, page hoặc tài liệu cần audit
inputs:
  - { key: context, description: "URL, screenshots, code, supported devices và tiêu chuẩn áp dụng", required: true }
outputs:
  - { key: findings, description: "Issue, user impact, criterion và evidence", required: true, type: list }
  - { key: backlog, description: "Cách sửa, priority và cách kiểm chứng", required: true, type: list }
roles:
  - { key: audit, name: Accessibility auditor, hint: keyboard, screen reader, contrast }
  - { key: product, name: Sản phẩm, hint: user impact, scope, acceptance, differ_from: [audit] }
  - { key: engineering, name: Kỹ thuật, hint: implementation, regression, test }
parallel: [[audit, product], [engineering]]
limits: { rounds: 2, turns: 8, timeout: 3h }
brief: [context]
---
1. Kiểm tra bằng tiêu chuẩn và user journey, ghi reproduction/evidence cho từng issue.
2. Xếp hạng theo barrier và số người bị ảnh hưởng, nêu quick win và structural fix.
3. Gọi `workflow_done` với findings và backlog.
