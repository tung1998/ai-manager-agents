---
key: feature
name: Làm tính năng
description: Từ yêu cầu tới code đã kiểm chứng. Lên kế hoạch, phản biện bằng model khác hãng, người dùng duyệt kế hoạch, làm trong worktree, review bằng model khác hãng, rồi build/test phải đạt.
input: Tính năng cần làm
inputs:
  - { key: outcome, description: "Tính năng cần làm (kết quả người dùng cần)", required: true }
  - { key: constraints, description: "Ràng buộc đã kiểm chứng" }
  - { key: done_when, description: "Tiêu chí xong" }
outputs:
  - { key: changes, description: "Đã thay đổi gì (file, hành vi)", required: true }
  - { key: checks_passed, description: "Build/test có đạt không", type: boolean, required: true }
roles:
  - key: ke-hoach
    name: Lập kế hoạch
    hint: design, architecture
    access: analyze
    prefer: { tier: strong }
  - key: phan-bien
    name: Phản biện
    hint: design review, finding risks
    access: analyze
    differ_from: [ke-hoach]
  - key: thuc-thi
    name: Thực thi
    hint: changing code
    access: edit
  - key: review
    name: Review
    hint: code review
    access: analyze
    differ_from: [thuc-thi]
    prefer: { tier: strong }
gates:
  - key: duyet-ke-hoach
    name: Người dùng duyệt kế hoạch
    kind: approve
    required: true
  - key: kiem-tra
    name: Build/test đạt
    kind: check
    required: true
limits: { rounds: 3, turns: 12, timeout: 4h, idle: 45m }
brief: [outcome, constraints, done_when]
---
1. Assign `ke-hoach` to make the plan: the outcome needed, the contract (API, data, behavior), risks, order of work. Go straight to the end state; do not split into intermediate phases without real dependencies. Do not pre-write how to change each function.
2. Ask `phan-bien` to check the plan. If there are points worth fixing, send them back to `ke-hoach` (`workflow_send`).
3. Open the `duyet-ke-hoach` gate with `workflow_gate` and a plan summary (enough for the person to decide without rereading the whole chat), then end the turn and wait for the person to approve. If rejected, revise the plan per the reason or call `workflow_done` saying why it stopped.
4. Once approved, assign `thuc-thi`, sending the approved plan as `outcome` and `constraints`.
5. Assign `review` to check the changes against the plan and contract (not line by line against the plan). If there are bugs, send them back to `thuc-thi`.
6. Open the `kiem-tra` gate with `workflow_gate` using the project's build/test command (`role: thuc-thi` to run it in its worktree). If it fails, send the errors to `thuc-thi` to fix, then check again.
7. Call `workflow_done`: `summary` is what was done, what the review said, the check results; `outputs.changes`, `outputs.checks_passed`.
