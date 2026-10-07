---
key: lam-tinh-nang
name: Làm tính năng
description: Từ yêu cầu tới code đã kiểm chứng. Lên kế hoạch, phản biện bằng model khác hãng, người dùng duyệt kế hoạch, làm trong worktree, review bằng model khác hãng, rồi build/test phải đạt.
input: Tính năng cần làm
roles:
  - key: ke-hoach
    name: Lập kế hoạch
    hint: thiết kế, kiến trúc
    access: analyze
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
gates:
  - key: duyet-ke-hoach
    name: Người dùng duyệt kế hoạch
    kind: approve
    required: true
  - key: kiem-tra
    name: Build/test đạt
    kind: check
    required: true
limits: { rounds: 3, turns: 12, timeout: 4h }
brief: [outcome, constraints, done_when]
---
1. Giao `ke-hoach` lập kế hoạch: kết quả cần đạt, contract (API, dữ liệu, hành vi), rủi ro, thứ tự làm. Đi thẳng tới trạng thái cuối, không chia nhiều phase trung gian khi không có phụ thuộc thật. Không viết sẵn cách sửa từng hàm.
2. Giao `phan-bien` soát kế hoạch. Có điểm đáng sửa thì gửi lại `ke-hoach` (`workflow_send`).
3. Mở cổng `duyet-ke-hoach` bằng `workflow_gate` kèm tóm tắt kế hoạch, rồi dừng lượt chờ người dùng duyệt.
4. Được duyệt thì giao `thuc-thi`, gửi kế hoạch đã duyệt làm `outcome` và `constraints`.
5. Giao `review` soát thay đổi so với kế hoạch và contract (không so từng dòng với kế hoạch). Có lỗi thì gửi lại `thuc-thi`.
6. Mở cổng `kiem-tra` bằng `workflow_gate` với lệnh build/test của project. Không đạt thì gửi lỗi cho `thuc-thi` sửa rồi kiểm tra lại.
7. Báo người dùng: đã làm gì, review nói gì, kết quả kiểm tra. Gọi `workflow_done`.
