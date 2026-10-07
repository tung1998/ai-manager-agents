---
key: bugfix
name: Sửa bug
description: Tìm nguyên nhân gốc trước khi sửa. Một agent điều tra (không sửa), một agent sửa, một agent khác hãng model kiểm chứng; build/test phải đạt.
input: Mô tả lỗi (log, cách tái hiện)
roles:
  - key: dieu-tra
    name: Điều tra
    hint: debug, đọc log, tìm nguyên nhân gốc
    access: analyze
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
limits: { rounds: 3, turns: 10, timeout: 3h }
brief: [outcome, context]
---
1. Giao `dieu-tra` tìm nguyên nhân gốc: tái hiện, đọc log và code, nêu bằng chứng (file:dòng). Chưa sửa gì.
2. Nguyên nhân chưa chắc thì hỏi tiếp `dieu-tra` (`workflow_send`); đừng để sửa theo phỏng đoán.
3. Giao `sua` sửa đúng nguyên nhân đã tìm, kèm test bắt được lỗi này nếu được.
4. Giao `kiem-chung` soát bản sửa: có đúng nguyên nhân không, có làm hỏng chỗ khác không.
5. Mở cổng `kiem-tra` bằng `workflow_gate` với lệnh build/test của project; không đạt thì gửi lỗi cho `sua`.
6. Báo người dùng: nguyên nhân, đã sửa gì, kiểm chứng ra sao. Gọi `workflow_done`.
