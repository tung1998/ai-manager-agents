---
key: giao-lai
name: Giao lại
description: Giao trọn việc đang làm cho một agent khác, kèm bản bàn giao đủ bối cảnh. Dùng khi việc cần quyền hay chuyên môn khác, hoặc khi lập kế hoạch ở đây rồi để agent khác làm.
input: Việc cần giao (có thể kèm @tên agent nhận)
roles:
  - key: nguoi-nhan
    name: Người nhận
    hint: thực thi, sửa code
    access: edit
limits: { rounds: 2, turns: 3, timeout: 2h }
brief: [outcome, constraints, current_option, tried, done_when]
---
1. Đọc lại cuộc chat và phần code liên quan để biết việc đang ở đâu.
2. Gọi `workflow_delegate` cho vai `nguoi-nhan`. Người nhận không biết gì về cuộc chat này, nên bản bàn giao phải tự đủ:
   - `outcome`: kết quả người dùng cần, không phải cách làm;
   - `constraints`: ràng buộc đã kiểm chứng;
   - `current_option`: phương án đang thử, nói rõ người nhận được phản biện;
   - `tried`: đã thử gì, vì sao bỏ;
   - `done_when`: tiêu chí xong;
   - thêm `files` (chỉ đường dẫn) và `must_not` nếu có.
   Không ghi chi tiết tới mức sửa file nào, hàm nào: người nhận đọc code rồi tự quyết.
3. Giữ đúng ý định của người dùng: chỉ điều tra thì ghi "không sửa file"; sửa lỗi thì "sửa lỗi"; refactor thì "refactor, không viết lại".
4. Báo người dùng đã giao cho ai rồi dừng lượt, không chờ. Khi người nhận xong, đối chiếu kết quả với tiêu chí xong, báo ngắn gọn, rồi gọi `workflow_done`.
