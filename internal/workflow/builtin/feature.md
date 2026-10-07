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
    hint: thiết kế, kiến trúc
    access: analyze
    prefer: { tier: strong }
  - key: phan-bien
    name: Phản biện
    hint: review thiết kế, tìm rủi ro
    access: analyze
    differ_from: [ke-hoach]
  - key: thuc-thi
    name: Thực thi
    hint: sửa code
    access: edit
  - key: review
    name: Review
    hint: review code
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
1. Giao `ke-hoach` lập kế hoạch: kết quả cần đạt, contract (API, dữ liệu, hành vi), rủi ro, thứ tự làm. Đi thẳng tới trạng thái cuối, không chia nhiều phase trung gian khi không có phụ thuộc thật. Không viết sẵn cách sửa từng hàm.
2. Hỏi `phan-bien` soát kế hoạch. Có điểm đáng sửa thì gửi lại `ke-hoach` (`workflow_send`).
3. Mở cổng `duyet-ke-hoach` bằng `workflow_gate` kèm tóm tắt kế hoạch (đủ để người dùng quyết mà không phải đọc lại cả cuộc chat), rồi dừng lượt chờ người dùng duyệt. Bị từ chối thì sửa kế hoạch theo lý do hoặc gọi `workflow_done` báo vì sao dừng.
4. Được duyệt thì giao `thuc-thi`, gửi kế hoạch đã duyệt làm `outcome` và `constraints`.
5. Giao `review` soát thay đổi so với kế hoạch và contract (không so từng dòng với kế hoạch). Có lỗi thì gửi lại `thuc-thi`.
6. Mở cổng `kiem-tra` bằng `workflow_gate` với lệnh build/test của project (`role: thuc-thi` để chạy trong worktree của nó). Không đạt thì gửi lỗi cho `thuc-thi` sửa rồi kiểm tra lại.
7. Gọi `workflow_done`: `summary` là đã làm gì, review nói gì, kết quả kiểm tra; `outputs.changes`, `outputs.checks_passed`.
