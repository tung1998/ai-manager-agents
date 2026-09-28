# Chat: tag để kéo agent vào nhóm

Ngày: 2026-09-28 · Trạng thái: chờ duyệt

## 1. Mục tiêu

Chat vẫn là **1-1** như hiện nay. Khi bạn **tag `@Agent`**, agent đó được kéo vào cuộc chat, trả lời và từ đó là thành viên của nhóm. Mọi người đọc chung một cuộc chat, giống bạn và mình đang nhắn ở đây.

Các lựa chọn đã chốt với người dùng (2026-09-28):
- Tin **không tag** ai: **agent đang chọn ở ô nhập** trả lời.
- **Agent được tag agent khác, nhưng hạn chế**: chỉ khi thật sự cần, ví dụ việc cần quyền hay chuyên môn mà nó không có. Lead thường tự làm được.

**Tiêu chí thành công**
- `@Dev sửa lỗi X` trong một cuộc chat với Trưởng nhóm: Dev trả lời ngay trong cuộc chat đó và thấy được những gì đã nói trước. Từ đó Dev có tên trong danh sách thành viên.
- Tag hai người trong một tin: hai agent trả lời **lần lượt**, người sau thấy câu trả lời của người trước.
- Mỗi agent vẫn **nối tiếp phiên Claude Code của chính nó**. Đến lượt mình, agent chỉ nhận các tin mới kể từ lần nói trước, có ghi tên người nói. Agent không phải đọc lại cả cuộc chat mỗi lượt.
- Agent tag agent khác thì có giới hạn số lượt chuyển. Không có vòng lặp nào chạy mãi.

**Ngoài phạm vi**
- Chat của Việc và khung tạo tự động hóa vẫn một agent, không tag.
- Việc (đội tự làm theo quy trình) giữ nguyên.

## 2. Thiết kế

### 2.1 Dữ liệu (migration 00028)

Bảng `conversation_agents`: thành viên của một cuộc chat, **mỗi agent một phiên riêng**.

| Cột | Ý nghĩa |
|---|---|
| `conversation_id`, `agent_id` | khóa chính kép; xóa theo khi cuộc chat bị xóa |
| `agent_name` | giữ tên khi agent bị xóa |
| `session_id`, `runtime` | phiên Claude Code của agent này trong cuộc chat |
| `last_message_id` | tin cuối cùng agent đã thấy |
| `context_tokens`, `context_window` | context sau lượt cuối của agent |
| `joined_at` | lúc vào nhóm |

- `conversations.agent_id` giữ nghĩa **agent mặc định**, là agent đang chọn ở ô nhập.
- Migration chép `agent_id`, `session_id`, `runtime` và context hiện có của mỗi cuộc chat thành một dòng thành viên. Các cuộc chat cũ vì vậy vẫn nối tiếp phiên như trước.
- `SetAgent` (đổi agent ở ô nhập) chỉ đổi agent mặc định. **Phiên của từng agent được giữ lại.** Đây là điểm khác với cách làm hiện nay: hiện đổi agent là xóa phiên.

### 2.2 Tag

- `chat.Mentions(text string, agents []storage.Agent) []storage.Agent`:
  - tìm `@Tên` hoặc `@key`, không phân biệt hoa thường và dấu cách (`@Trưởng nhóm`, `@truong-nhom`, `@dev`);
  - ưu tiên tên dài nhất khớp được, theo thứ tự xuất hiện, bỏ trùng;
  - agent không thuộc project thì bỏ qua;
  - bỏ qua `@` nằm trong code (` `…` ` và khối ```).
- **Người trả lời một tin của người dùng**: các agent được tag, theo thứ tự tag. Không tag ai thì là agent mặc định.
- Người được tag lần đầu thì được thêm vào `conversation_agents`.

### 2.3 Lượt trả lời nối tiếp

- `SendWithContext` vẫn trả về **một** `Turn` cho người trả lời đầu tiên.
- Khi một lượt xong mà còn người trả lời tiếp, engine tạo `Turn` kế. Sự kiện `done` của lượt trước mang `next_turn_id` để dashboard theo tiếp. Mỗi lượt vẫn là một job `chat_turn`, nên chi phí tính riêng cho từng agent.
- **Agent tag agent khác:**
  - Sau câu trả lời của agent A, nếu có `@B` thì B được thêm vào cuối hàng đợi. A không tự tag chính mình.
  - Một tin của người dùng cho phép tối đa **2 lượt chuyển do agent tạo ra**.
  - Quá giới hạn thì dừng và ghi một dòng hệ thống: "Đã dừng chuyển tiếp: quá 2 lượt, hãy tag lại nếu cần".
- **Dừng** (`cancel`) sẽ dừng lượt đang chạy và bỏ hết hàng đợi.
- Đang có lượt chạy thì không nhận tin mới (`ErrBusy`), như hiện nay.

### 2.4 Agent thấy gì

**Lần đầu vào nhóm** (chưa có phiên):
- 30 tin gần nhất, dạng transcript. Mỗi tin ghi tên người nói (`[Người dùng]`, `[Trưởng nhóm]`, `[Bạn]`).
- Kèm câu: "Bạn vừa được @… kéo vào cuộc chat".

**Các lượt sau:**
- Nối tiếp phiên của chính agent (`--resume`).
- Prompt chỉ gồm các tin **sau `last_message_id`**, mỗi tin có tên người nói, rồi đến tin đang chờ trả lời.
- Nếu Claude Code không còn phiên đó, office gửi lại transcript như lần đầu (giống cách xử lý hiện nay).

**System prompt thêm:**
- tên các thành viên cùng mức quyền của từng người;
- quy tắc: *"Chỉ tag @agent khác khi thật sự cần (việc cần quyền hay chuyên môn bạn không có), ghi rõ cần họ làm gì. Việc bạn tự làm được thì tự làm."*

Kết nối kiểu API làm tương tự: đưa lịch sử qua `HistoryFor`, câu của agent khác được ghi tên người nói.

### 2.5 Giao diện

- **Ô nhập:**
  - gõ `@` hiện danh sách agent của project (tên và nhãn quyền), chọn để chèn tag;
  - ô chọn agent là người trả lời mặc định.
- **Thanh trên cuộc chat:** avatar các thành viên. Rê chuột vào một avatar thấy tên, quyền và context của agent đó.
- **Tin nhắn:**
  - tên agent đã hiện sẵn;
  - trong nội dung, `@Tên` được tô màu;
  - lượt kế tiếp hiện "Dev đang trả lời…" ngay sau lượt trước.
- **Vòng context:** hiện context của agent mặc định (theo `conversation_agents`).
- **Danh sách chat:** cuộc chat có từ 2 thành viên thì hiện avatar chồng nhau.

### 2.6 API

- `POST /api/conversations/:id/messages`: như cũ. Người trả lời được suy ra từ tag và agent mặc định.
- `GET /api/conversations/:id`: trả thêm `members` gồm `agent_id`, `agent_name`, `level`, `context_tokens`, `context_window`.
- SSE: sự kiện `done` có `next_turn_id`, sự kiện `status` có `agent_name`.

### 2.7 Lỗi và giới hạn

- Tag tên không có trong project thì coi như văn bản thường. Tag trúng agent của project khác thì không được tính.
- Một lượt lỗi, ví dụ hết quota hay agent bị xóa: ghi tin lỗi của agent đó rồi **chạy tiếp người sau** trong hàng đợi.
- Tối đa 4 người trả lời cho một tin của người dùng, tính cả lượt chuyển do agent tạo ra.
- Mỗi lượt vẫn đi qua kiểm tra ngân sách như hiện nay.

## 3. Kiểm thử

- `Mentions`:
  - tên có dấu hoặc dấu cách, key, không phân biệt hoa thường;
  - bỏ qua code, bỏ trùng;
  - ưu tiên tên dài nhất (`@Dev` và `@Dev Lead`).
- Engine, với một `claude` giả:
  - không tag thì agent mặc định trả lời;
  - `@Dev` thì Dev trả lời và được thêm làm thành viên;
  - `@A @B` thì A trả lời trước, B sau, và B nhận được câu của A;
  - lượt sau của A chỉ nhận các tin mới, kèm `--resume` đúng phiên của A;
  - agent tag agent khác thì chuyển, quá 2 lượt thì dừng kèm dòng hệ thống;
  - Dừng thì bỏ hàng đợi;
  - lượt lỗi thì vẫn chạy người sau.
- Migration: cuộc chat cũ có đúng một thành viên, giữ phiên cũ.
- `SetAgent` đổi agent mặc định nhưng giữ phiên của các agent.

## 4. Thứ tự làm

1. Migration, repo thành viên, `Mentions`.
2. Engine: hàng đợi người trả lời, phiên của từng agent, chỉ gửi tin mới, chuyển tiếp có giới hạn, prompt.
3. API và SSE.
4. Dashboard: gợi ý khi gõ `@`, avatar thành viên, tô màu tag, theo lượt kế tiếp.
5. Tài liệu (ADR-044).

Ghi chú: agent Trưởng nhóm của các template hiện để quyền **Chỉ đọc**. Người dùng kỳ vọng "lead nắm toàn bộ quyền", nên sẽ hỏi riêng có nâng quyền mặc định của lead trong template không. Việc này nằm ngoài spec này.
