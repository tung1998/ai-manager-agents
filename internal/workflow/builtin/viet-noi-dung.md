---
key: viet-noi-dung
name: Viết nội dung
description: Viết bài (blog, Facebook, email cho khách), có agent khác hãng model phản biện, người dùng duyệt trước khi đăng hay gửi.
input: Chủ đề, đối tượng đọc, kênh đăng
roles:
  - key: viet
    name: Người viết
    hint: viết nội dung
    access: analyze
  - key: phan-bien
    name: Phản biện
    hint: biên tập, kiểm tra thông tin
    access: analyze
    differ_from: [viet]
gates:
  - key: duyet
    name: Người dùng duyệt bản cuối
    kind: approve
    required: true
limits: { rounds: 3, turns: 8, timeout: 2h }
brief: [outcome, constraints]
---
1. Giao `viet` viết bản nháp: `outcome` là mục đích của bài và người đọc; `constraints` là kênh, độ dài, giọng văn, điều không được nói.
2. Giao `phan-bien` soát bản nháp: thông tin sai, chỗ khó hiểu, chỗ lệch giọng. Gửi nhận xét lại cho `viet` (`workflow_send`) tới khi ổn.
3. Mở cổng `duyet` bằng `workflow_gate` kèm bản cuối, rồi dừng lượt chờ người dùng duyệt.
4. Được duyệt thì mới đăng hay gửi (qua công cụ của project, vẫn theo quyền của bạn). Báo người dùng và gọi `workflow_done`.
