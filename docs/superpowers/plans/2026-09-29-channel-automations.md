# Kênh chat là nguồn của tự động hóa — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans.

**Goal:** Kênh (Telegram/Discord) chỉ còn là *kết nối* (token, trạng thái, danh sách được phép, câu trả lời khi không quy tắc nào khớp). Việc xử lý tin là **tự động hóa** có nguồn `telegram`/`discord`: mỗi quy tắc chọn kênh, điều kiện (từ khóa, phạm vi AI) và hành động (agent trả lời trong chat, giao Việc, chạy script), rồi kết quả được gửi lại vào đúng chat.

## Rulings
- Nguồn `telegram` | `discord` (CHECK của DB đã có). Config thêm `channel_id`, `keywords []string` (chứa một trong các từ, không phân biệt hoa thường; trống = mọi tin), `scope` (mô tả chủ đề; có thì model nhanh hỏi YES/NO).
- **Chọn quy tắc:** các tự động hóa đang bật của kênh, theo thứ tự tạo; **quy tắc đầu tiên khớp thắng**. Từ khóa xét trước (miễn phí), phạm vi AI chỉ gọi khi từ khóa đã khớp. Không quy tắc nào khớp → gửi `refusal` của kênh (trống = im lặng), ghi job `skipped` mã `no_rule`.
- **Chạy:** `trigger.Runner.Enqueue(a, kind, payload, dedupe)` — dùng chung giới hạn/số lượt/trần chi phí/tự tắt/escalate của tự động hóa. Payload JSON: `message, user, user_id, chat_id, channel_id, conversation_id`.
- **Hành động:**
  - `chat` (hiện là "Trả lời trong chat"): mỗi (chat ngoài, quy tắc) một conversation (khóa thread `chatID#automationID`), purpose `channel` → luôn **không công cụ**, mode read. Prompt mặc định `{{message}}`.
  - `task`: gửi ngay "Đã nhận, đội đang xử lý", xong Việc gửi kết quả (Result, không thì Detail).
  - `script`: gửi stdout (bỏ dòng `@@agent:`); nếu escalate thì câu trả lời của agent được gửi tiếp.
- **Gửi lại:** `Runner.SetOnReply(func(ctx, origin Job, reply string, err error, final bool))`; origin là job mang payload kênh (với job escalate là job cha). `Executor.RunChat` trả thêm câu trả lời cuối.
- Giới hạn 3 tin chờ mỗi chat giữ nguyên; nhả khi `final`. "Đang gõ" gửi đều tới khi xong.
- Kênh cũ: khi khởi động, kênh nào chưa chuyển (settings `channels_rules_v1`) được tạo một quy tắc "Trả lời" từ agent/phạm vi cũ. Cột cũ của bảng channels để nguyên, không dùng nữa. Ngữ cảnh chat cũ không mang sang (khóa thread đổi).
- Agent đề xuất tự động hóa (`trigger.Spec`) vẫn chỉ lịch/webhook — để sau.

## Tasks
1. Runner: RunChat trả reply; conversation từ payload cho nguồn kênh; `{{message}}`/`{{user}}`; OnReply cho chat/task/script/escalate. Test trong `internal/trigger`.
2. API: `applyAutomation` cho nguồn kênh (kênh tồn tại, cùng project, đúng loại), DTO config. Test.
3. Manager: chọn quy tắc, thread theo quy tắc, enqueue, nhận reply gửi lại, giới hạn tin chờ, không khớp → refusal; chuyển kênh cũ. Test với runner giả.
4. UI: nguồn "Tin nhắn kênh" trong form tự động hóa; tab Kênh chỉ còn kết nối + danh sách quy tắc; locale.
5. ADR-049.
