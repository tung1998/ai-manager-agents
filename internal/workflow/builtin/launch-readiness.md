---
key: launch-readiness
name: Kiểm tra sẵn sàng ra mắt
description: Đánh giá sản phẩm, kỹ thuật, hỗ trợ và truyền thông theo một checklist chung trước khi phát hành.
input: Release hoặc thay đổi cần quyết định go/no-go
inputs:
  - { key: context, description: "Scope, target date, rollout và rollback plan", required: true }
  - { key: done_when, description: "Tiêu chí hoàn thành và tín hiệu thành công" }
outputs:
  - { key: checklist, description: "Các mục đã kiểm và bằng chứng", required: true, type: list }
  - { key: verdict, description: "go | hold | go_with_guardrails", required: true }
roles:
  - key: product
    name: Sản phẩm
    hint: user value, scope, acceptance, experiment
  - key: engineering
    name: Kỹ thuật
    hint: tests, observability, rollout, rollback
    access: analyze
    differ_from: [product]
  - key: support
    name: Hỗ trợ
    hint: runbook, FAQ, escalation, customer impact
  - key: growth
    name: Nội dung và tăng trưởng
    hint: messaging, docs, audience, measurement
parallel: [[product, engineering], [support, growth]]
gates:
  - key: go-no-go
    name: Người phụ trách duyệt go/no-go
    kind: approve
    required: true
limits: { rounds: 2, turns: 10, timeout: 3h, idle: 45m }
brief: [done_when, context]
---
1. Giao bốn vai trò kiểm tra phần của mình bằng evidence, ghi missing thay vì đoán đã xong.
2. Hợp nhất checklist, dependency, risk, owner và tín hiệu theo dõi sau launch; phân biệt blocker với việc có thể làm sau.
3. Mở gate `go-no-go` với scope, rollout, rollback, guardrails và các mục còn thiếu.
4. Gọi `workflow_done` với checklist, verdict và điều kiện tiếp tục/hoãn phát hành.
