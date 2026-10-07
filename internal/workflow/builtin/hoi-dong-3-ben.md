---
key: hoi-dong-3-ben
name: Hội đồng 3 bên
description: Tam quyền phân lập. Lập kế hoạch đặt mục tiêu và kế hoạch, Thực thi làm theo kế hoạch đã thông qua, Giám sát kiểm tra và có quyền phủ quyết. Mọi quyết định cần 2/3 đồng ý.
input: Mục tiêu cần làm
roles:
  - key: lap-ke-hoach
    name: Lập kế hoạch
    hint: lập pháp, mục tiêu, quy tắc, kế hoạch
    access: analyze
  - key: thuc-thi
    name: Thực thi
    hint: hành pháp, làm việc, sửa code
    access: edit
  - key: giam-sat
    name: Giám sát
    hint: tư pháp, kiểm tra bằng chứng, phủ quyết
    access: analyze
    differ_from: [thuc-thi]
vote:
  roles: [lap-ke-hoach, thuc-thi, giam-sat]
  quorum: 2
  veto: [giam-sat]
limits: { rounds: 4, turns: 16, timeout: 3h }
brief: [outcome, constraints, done_when]
---
1. Giao `lap-ke-hoach` viết kế hoạch: mục tiêu, phạm vi, tiêu chí xong, rủi ro. Kế hoạch nêu kết quả và ràng buộc, không viết sẵn cách sửa từng file.
2. Đưa kế hoạch ra biểu quyết bằng `workflow_vote`. Không thông qua thì gửi lại cho `lap-ke-hoach` (`workflow_send`) kèm các ý kiến phản đối, tối đa 2 lần; vẫn không qua thì dừng và báo người dùng.
3. Thông qua thì giao `thuc-thi` làm đúng kế hoạch đó.
4. Giao `giam-sat` kiểm tra kết quả so với kế hoạch và tiêu chí xong, dựa trên bằng chứng (diff, kết quả test), không dựa trên lời khẳng định.
5. Đưa kết quả ra biểu quyết nghiệm thu. Bị phủ quyết hay không đủ phiếu thì gửi lại `thuc-thi` các điểm cần sửa.
6. Báo người dùng: kế hoạch đã thông qua, đã làm gì, kết quả biểu quyết. Gọi `workflow_done`.
