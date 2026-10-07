---
key: handoff
name: Giao lại
description: Giao trọn việc đang làm cho một agent khác, kèm bản bàn giao đủ bối cảnh. Dùng khi việc cần quyền hay chuyên môn khác, hoặc khi lập kế hoạch ở đây rồi để agent khác làm.
input: Việc cần giao (có thể kèm @tên agent nhận)
inputs:
  - { key: outcome, description: "Kết quả cần đạt", required: true }
  - { key: constraints, description: "Ràng buộc đã kiểm chứng" }
  - { key: done_when, description: "Tiêu chí xong" }
roles:
  - key: nguoi-nhan
    name: Người nhận
    hint: thực thi, sửa code
    access: edit
limits: { rounds: 2, turns: 3, timeout: 2h, idle: 45m }
brief: [outcome, constraints, current_option, tried, done_when]
---
1. Đọc phần bối cảnh của cuộc chat đã gọi (cuối tin đầu tiên) và phần code liên quan để biết việc đang ở đâu.
2. Gọi `workflow_delegate` cho vai `nguoi-nhan`. Người nhận không biết gì về cuộc chat kia, nên bản bàn giao phải tự đủ:
   - `outcome`: kết quả người dùng cần, không phải cách làm;
   - `constraints`: ràng buộc đã kiểm chứng;
   - `current_option`: phương án đang thử, nói rõ người nhận được phản biện;
   - `tried`: đã thử gì, vì sao bỏ;
   - `done_when`: tiêu chí xong;
   - thêm `files` (chỉ đường dẫn) và `must_not` nếu có.
   Không ghi chi tiết tới mức sửa file nào, hàm nào: người nhận đọc code rồi tự quyết.
3. Giữ đúng ý định của người dùng: chỉ điều tra thì ghi "không sửa file"; sửa lỗi thì "sửa lỗi"; refactor thì "refactor, không viết lại".
4. Ghi ngắn đã giao cho ai rồi dừng lượt. Khi người nhận xong, đối chiếu kết quả với tiêu chí xong; chưa đạt thì gửi tiếp các điểm thiếu (`workflow_send`).
5. Gọi `workflow_done`: `summary` là người nhận đã làm gì, kết quả so với tiêu chí xong, việc còn lại (nếu có).
