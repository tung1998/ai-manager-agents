# Trang tạo tự động hóa có chat và chat ở góc (ADR-042 gđ1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans.

**Goal:** Trang tạo/sửa tự động hóa chia đôi, form bên trái và chat bên phải; AI điền được form. Thêm chat ở góc chính là Chat của project, kèm ngữ cảnh trang.

**Spec:** `docs/DECISIONS.md` ADR-042.

## Global Constraints
- Không tạo hệ thống chat mới: dùng lại ChatPanel và chat engine.
- Khối điền form viết theo dạng ` ```automation {json} ``` `. `context` tối đa 8KB, và luôn được đánh dấu là dữ liệu.
- Chạy thử chỉ admin gọi được, timeout tối đa 120 giây.
- Chữ trên dashboard phải qua i18n. Commit trên `main`.

## Review Focus
1. Khối `automation` chứa JSON hỏng, hoặc trường lạ: bỏ qua, không làm vỡ form.
2. Người dùng gửi tin lúc đang sửa dở: bản nháp gửi kèm phải là trạng thái mới nhất.
3. Hai tab cùng mở một tự động hóa: cả hai dùng một chat, không tạo trùng.
4. Chat của tự động hóa không hiện lẫn vào danh sách Chat của project.
5. Ngữ cảnh chứa chỉ dẫn độc: agent thấy nó được đánh dấu là dữ liệu.

### Task 1: Backend
- Migration 00022; thêm `Conversation.Purpose`, `Conversation.AutomationID`, `Message.Context`.
- Repo:
  - `ListConversations` bỏ qua các chat có `purpose != ''`;
  - thêm `AutomationConversation(ctx, id)`;
  - thêm `LinkAutomation(ctx, convID, automationID)`.
- Engine:
  - thêm `SendWithContext(ctx, conv, text, context, att)`; `Send` gọi hàm này với context rỗng;
  - prompt được bọc ngữ cảnh;
  - system prompt có thêm hướng dẫn khối `automation` khi `purpose=automation`.
- API:
  - `sendMessage` nhận `context`;
  - `POST /api/projects/:id/conversations` nhận `purpose`;
  - `POST /api/automations/:id/conversation`;
  - tạo và sửa tự động hóa nhận `conversation_id` để gắn chat;
  - `POST /api/projects/:id/automations/test-script`.
- Test:
  - repo: chat có purpose không hiện trong danh sách; gắn chat rồi tìm lại được;
  - engine: prompt có ngữ cảnh kèm nhãn dữ liệu; system prompt của chat automation có hướng dẫn khối;
  - API: Chạy thử trả output và mã thoát, member bị 403; tạo tự động hóa có `conversation_id` thì chat được gắn.

### Task 2: Dashboard, trang tạo và sửa
- Tách `AutomationForm.vue` (dùng `v-model` cho bản nháp, có prop `highlight` là danh sách ô vừa đổi), bỏ panel trượt `AutomationEditor`.
- Tạo trang `automations/new.vue` và `automations/[aid]/edit.vue`, chia đôi bằng `lg:grid-cols-2`.
- `ChatPanel` thêm các prop:
  - `automationId` và `purpose`: mở hoặc tạo chat của tự động hóa;
  - `compact`: không có danh sách cuộc trò chuyện, chiếm hết chiều cao khung chứa;
  - `pageContext: () => string`.
- `ChatPanel` phát sự kiện `automation-patch` khi câu trả lời có khối ` ```automation `.
- Các nút "Tạo tự động" và "Sửa" dẫn sang hai trang mới.

### Task 3: Chat ở góc
- `FloatingChat.vue` trong layout:
  - hiện khi đang ở trong project, trừ tab chat và trang tạo/sửa tự động hóa;
  - mở `USlideover` bên phải chứa `ChatPanel compact` với `pageContext` dựng từ route (trang, tab, id đối tượng, tên nếu có).

### Task 4: Tài liệu, build, kiểm tra
- Chạy check-i18n, typecheck, build. Khởi động lại office.
- Kiểm tra API bằng curl: Chạy thử; tạo chat của tự động hóa; gửi tin có `context`, kiểm tra tin được lưu kèm `context`. Không gọi AI thật.
