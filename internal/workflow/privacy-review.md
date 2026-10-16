---
key: privacy-review
name: Review quyền riêng tư
category: Security & Operations
description: Kiểm tra data flow, mục đích sử dụng, lưu trữ và quyền truy cập trước khi ra mắt tính năng.
input: Tính năng hoặc tích hợp có xử lý dữ liệu
inputs:
  - { key: context, description: "Data flow, loại dữ liệu, purpose, retention và bên nhận", required: true }
  - { key: constraints, description: "Policy, khu vực pháp lý và yêu cầu consent" }
outputs:
  - { key: findings, description: "Risk, gap, evidence và mitigation", required: true, type: list }
  - { key: verdict, description: "go | changes_required | no_go", required: true }
roles:
  - { key: privacy, name: Privacy, hint: purpose, minimization, retention, rights }
  - { key: security, name: Security, hint: access, encryption, logging, differ_from: [privacy] }
  - { key: product, name: Sản phẩm, hint: user expectation, UX, fallback }
parallel: [[privacy, security], [product]]
gates:
  - { key: signoff, name: Duyệt privacy risk, kind: approve, required: true }
limits: { rounds: 3, turns: 10, timeout: 3h }
brief: [constraints, context]
---
1. Vẽ data flow và kiểm tra purpose limitation, minimization, retention, consent và deletion.
2. Nêu rõ risk chưa giải quyết, mitigation có owner và evidence cần bổ sung.
3. Mở gate `signoff`; sau khi duyệt gọi `workflow_done` với findings và verdict.
