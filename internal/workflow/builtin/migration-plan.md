---
key: migration-plan
name: Lập kế hoạch migration
category: Engineering
description: Lập kế hoạch chuyển dữ liệu hoặc hệ thống theo từng bước, có kiểm chứng, cutover và rollback.
input: Hệ thống hoặc dữ liệu cần migration
inputs:
  - { key: context, description: "Nguồn, đích, volume, downtime, owner và constraint", required: true }
  - { key: outcome, description: "Trạng thái đích cần đạt" }
outputs:
  - { key: plan, description: "Runbook theo phase với kiểm chứng", required: true, type: list }
  - { key: risks, description: "Rủi ro mất dữ liệu, downtime và rollback", required: true, type: list }
roles:
  - { key: data, name: Dữ liệu, hint: "mapping, integrity, reconciliation" }
  - { key: platform, name: Nền tảng, hint: "tooling, capacity, rollback", differ_from: [data] }
  - { key: owner, name: Chủ hệ thống, hint: "business cutover, acceptance" }
parallel: [[data, platform], [owner]]
gates:
  - { key: cutover, name: Duyệt cutover, kind: approve, required: true }
limits: { rounds: 3, turns: 10, timeout: 4h }
brief: [outcome, context]
---
1. Xác định invariant, mapping, rehearsal và cách đối soát trước khi chạm production.
2. Viết runbook, checkpoint, owner, abort condition và rollback.
3. Mở gate `cutover`; sau khi duyệt gọi `workflow_done` với plan và risks.
