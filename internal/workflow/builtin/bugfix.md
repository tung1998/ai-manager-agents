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
    hint: debug, đọc log, tìm nguyên nhân gốc
    access: analyze
    prefer: { tier: strong }
  - key: sua
    name: Sửa
    hint: sửa code
    access: edit
  - key: kiem-chung
    name: Kiểm chứng
    hint: review, test
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
1. Giao `dieu-tra` tìm nguyên nhân gốc: tái hiện, đọc log và code, nêu bằng chứng (file:dòng). Chưa sửa gì.
2. Nguyên nhân chưa chắc thì hỏi tiếp `dieu-tra` (`workflow_send`); đừng để sửa theo phỏng đoán.
3. Giao `sua` sửa đúng nguyên nhân đã tìm, kèm test bắt được lỗi này nếu được.
4. Giao `kiem-chung` soát bản sửa: có đúng nguyên nhân không, có làm hỏng chỗ khác không.
5. Mở cổng `kiem-tra` bằng `workflow_gate` với lệnh build/test của project (`role: sua`); không đạt thì gửi lỗi cho `sua`.
6. Gọi `workflow_done`: `summary` là nguyên nhân, đã sửa gì, kiểm chứng ra sao; `outputs.root_cause`, `outputs.fix`, `outputs.checks_passed`.
