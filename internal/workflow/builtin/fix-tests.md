---
key: fix-tests
name: Sửa tới khi test xanh
description: Chạy lệnh kiểm tra; chưa đạt thì một agent sửa theo lỗi rồi chạy lại, tối đa 3 vòng. Chạy theo các bước, không cần agent điều phối.
input: "Lệnh kiểm tra, ví dụ: go test ./..."
inputs:
  - { key: command, description: "Lệnh build/test cần đạt", required: true }
outputs:
  - { key: passed, description: "Lệnh có đạt không", type: boolean, required: true }
roles:
  - key: sua
    name: Sửa
    hint: sửa code theo lỗi build/test
    access: edit
limits: { turns: 6, timeout: 2h, idle: 30m }
steps:
  - id: kiem-tra
    type: check
    name: Chạy kiểm tra
    command: "{{input.command}}"
    next: dat
    else: sua
    max_loops: 3
    position: { x: 0, y: 0 }
  - id: sua
    type: agent
    name: Sửa theo lỗi
    role: sua
    prompt: |
      Lệnh `{{input.command}}` chưa đạt:
      {{steps.kiem-tra.output}}

      Tìm nguyên nhân gốc và sửa code cho tới khi lệnh đạt. Không tắt test, không bỏ qua lỗi.
    next: kiem-tra
    position: { x: 320, y: 160 }
  - id: dat
    type: end
    name: Đạt
    summary: "Lệnh `{{input.command}}` đã đạt.\n{{steps.sua.output}}"
    outputs:
      passed: "true"
    position: { x: 320, y: -120 }
---
Quy trình chạy theo các bước ở phần đầu (steps).
