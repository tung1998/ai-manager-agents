---
key: bugfix
name: Sửa bug
description: Tìm nguyên nhân gốc trước khi sửa. Một agent điều tra (không sửa), một agent sửa, một agent khác hãng model kiểm chứng; build/test phải đạt.
input: Mô tả lỗi (log, cách tái hiện)
inputs:
  - { key: context, description: "Mô tả lỗi (log, cách tái hiện)", required: true }
  - { key: outcome, description: "Hành vi đúng mong muốn" }
outputs:
  - { key: root_cause, description: "Nguyên nhân gốc kèm bằng chứng (file:dòng)", required: true }
  - { key: fix, description: "Đã sửa gì", required: true }
  - { key: checks_passed, description: "Build/test có đạt không", type: boolean, required: true }
roles:
  - key: dieu-tra
    name: Điều tra
    hint: debugging, reading logs, finding the root cause
    access: analyze
    prefer: { tier: strong }
  - key: sua
    name: Sửa
    hint: fixing code
    access: edit
  - key: kiem-chung
    name: Kiểm chứng
    hint: review, testing
    access: analyze
    differ_from: [sua]
gates:
  - key: kiem-tra
    name: Build/test đạt
    kind: check
    required: true
limits: { rounds: 3, turns: 10, timeout: 3h, idle: 45m }
brief: [outcome, context]
---
1. Assign `dieu-tra` to find the root cause: reproduce, read logs and code, give evidence (file:line). Change nothing yet.
2. If the cause is still uncertain, ask `dieu-tra` more (`workflow_send`); do not let a fix be based on a guess.
3. Assign `sua` to fix exactly the cause found, with a test that catches this bug if possible.
4. Assign `kiem-chung` to check the fix: does it address the cause, does it break anything else.
5. Open the `kiem-tra` gate with `workflow_gate` using the project's build/test command (`role: sua`); if it fails, send the errors to `sua`.
6. Call `workflow_done`: `summary` is the cause, what was fixed, how it was verified; `outputs.root_cause`, `outputs.fix`, `outputs.checks_passed`.
