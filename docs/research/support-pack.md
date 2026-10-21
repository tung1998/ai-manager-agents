# Thiết kế: gói Chăm sóc khách hàng (`support`)

Bản nháp để hội đồng xét, chưa phải ADR. Cụ thể hóa §3.1 của `feature-packs.md`. Nền: ADR-048/049 (bot hai chiều, quy tắc là tự động hóa), ADR-161 (gói tính năng theo project), ADR-090 (ra ngoài qua thẻ duyệt).

## 1. Yêu cầu

1. **Ticket**: mỗi vụ việc của khách là một ticket, có toàn bộ lịch sử trao đổi (tin khách, nháp AI, câu đã gửi, ghi chú nội bộ), trạng thái, người phụ trách.
2. **Kênh**: bot Discord/Telegram, **Crisp**, **form** (web, Google Form…) và **API** cho hệ thống khác, ngay từ bước đầu.
3. **Phương án phản hồi**: tài liệu, file, link, câu trả lời mẫu; AI tra cứu nhanh, không mở file mỗi lần.
4. **Hai loại case**: **tự trả lời** (AI gửi luôn) và **chờ người xác nhận** (AI soạn nháp, người duyệt rồi gửi).
5. **Trigger ra ngoài**: sự kiện của ticket (mới, đã trả lời, chờ duyệt, đóng…) bắn webhook / gọi API / chạy tự động hóa.
6. **Tách module**: mọi thứ nằm trong gói; gói tắt thì luồng hiện tại (bot, chat, tự động hóa) chạy y như trước.
7. **Về sau tổng quát**: API gọi vào để tạo/nối chat dùng được cả ngoài gói (§3.8).
8. Không sửa code khi thêm loại case, phương án, hook, kênh form: tất cả là dữ liệu cấu hình.

## 2. Hiện trạng dùng lại được

| Có sẵn | Ở đâu | Dùng cho |
|---|---|---|
| Bot Discord/Telegram, mỗi chat ngoài = conversation | `internal/channels/manager.go:229` (`handle`), ADR-048 | kênh vào/ra |
| Quy tắc bot = tự động hóa | ADR-049, `internal/trigger` | luồng cũ khi gói tắt |
| Tin nhắn + FTS5 không dấu | `migrations/sqlite/00046_messages_fts.sql` | lịch sử, tìm kiếm |
| Thẻ duyệt theo thứ tự | ADR-160 | case chờ người |
| Tự động hóa `script`/`chat`/`workflow`, nguồn `schedule`/`webhook` (cứng trong `internal/trigger/spec.go:40`) | `internal/trigger` | hành động khi có sự kiện |
| Gói tính năng, "thiếu key = bật" | `internal/features/features.go:31,67` | bật/tắt gói |

Thiếu: ticket, kho phương án có chỉ mục, phân loại case, kênh Crisp/form/API, nguồn sự kiện nội bộ cho tự động hóa.

## 3. Thiết kế

### 3.1 Dữ liệu (migration riêng, tiền tố `support_`)

| Bảng | Cột chính |
|---|---|
| `support_sources` | id, project_id, kind (`bot`/`crisp`/`form`/`api`), name, config (bot: channel_id; crisp: website_id; form: ánh xạ trường), secret (mã hóa bằng secrets box như token bot), inbound_token, reply_via (`same`/`hook`/`email`/`none`), enabled |
| `support_tickets` | id, project_id, source_id, external_thread (session Crisp, chat id bot, id form…), customer_id, conversation_id, subject, status (`new`/`open`/`waiting_customer`/`waiting_approval`/`resolved`/`closed`), case_type, assignee, priority, tags, first_response_at, resolved_at |
| `support_events` | id, ticket_id, kind (`customer_msg`/`ai_draft`/`sent`/`note`/`status`/`assign`/`hook`), body, attachments, actor, message_id, external_id (chống trùng), created_at. **Lịch sử ticket**, chỉ thêm |
| collection `customers` (§7) | id, project_id, name, contacts (theo nguồn), tags, notes, external_ref |
| collection `playbooks` (§7) + `support_chunks` (FTS5) | tài liệu/file/link/câu mẫu, case_type, hash, synced_at; đoạn đã cắt để tra cứu |
| `support_case_types` | key, name, mode (`auto`/`approve`/`human`), match (từ khóa, mô tả cho AI), playbook_ids, sla_minutes |
| `support_hooks` | event, kind (`webhook`/`automation`), url/automation_id, secret, filter, enabled, nhật ký gửi |

Bảng có sẵn từ migration nhưng gói tắt thì không ai đọc/ghi: không ảnh hưởng gì.

### 3.2 Kênh (adapter của gói)

Mọi kênh quy về một dạng chung rồi đi chung một luồng:

```go
// support.Inbound: một tin từ bên ngoài, đã chuẩn hóa.
type Inbound struct {
    SourceID, ExternalThread, ExternalID string // ExternalID: chống nhận trùng khi bên ngoài gửi lại
    Customer  Contact                           // tên, email, phone, id ở nguồn
    Text      string
    Attachments []attach.File
    Meta      map[string]any                    // trường form, url trang, …
}
// support.Outbound: gửi câu trả lời về đúng nơi.
type Outbound interface { Send(ctx, t Ticket, text string, files []attach.File) (externalID string, err error) }
```

| Kênh | Vào | Ra |
|---|---|---|
| **Bot** Discord/Telegram | gói nhận tin của kênh được gán cho nguồn `bot` (§3.7) | `channels.Manager` gửi như hiện nay |
| **Crisp** | webhook Crisp `message:send` (khách gửi) → `POST /api/support/in/{source_id}`, kiểm chữ ký Crisp | Crisp REST API `POST /website/{website_id}/conversation/{session_id}/message` với plugin token; bỏ qua tin do chính office gửi (tránh lặp) |
| **Form** | `POST /api/support/in/{source_id}` (token trong URL hoặc header), ánh xạ trường form → khách + nội dung; nhận JSON và form-urlencoded | form không có kênh trả về: `reply_via=hook` (sự kiện `ticket.replied` để hệ thống người dùng gửi email) hoặc email qua MCP |
| **API** | như form, có thêm `external_thread` để nối tiếp hội thoại; trả `{ticket_id, conversation_id}` | `hook`, hoặc bên gọi lấy câu trả lời qua `GET .../tickets/{id}/events?after=` (polling) |

### 3.3 Luồng một tin khách

```
Inbound (bot / Crisp / form / API)
 → chống trùng theo ExternalID
 → tìm ticket mở theo (source, external_thread), không có thì tạo (+ conversation để agent làm việc)
 → ghi support_events(customer_msg), bắn ticket.created / ticket.message
 → phân loại case: từ khóa trước (0 token), không khớp thì model nhanh
 → agent tra support_kb_search + lịch sử ticket + hồ sơ khách, soạn trả lời có nguồn, tự chấm độ tự tin
 → theo mode của case:
     auto     → gửi qua Outbound (độ tự tin thấp / không nguồn → hạ xuống approve)
     approve  → thẻ duyệt trong Hộp thư (waiting_approval); người sửa / Gửi / Bỏ
     human    → chỉ tóm tắt + gợi ý, báo người phụ trách
 → ghi sent, bắn ticket.replied
```

Chạy agent bằng `trigger.Runner.Enqueue` như quy tắc bot: dùng chung giới hạn lượt, trần chi phí, tự tắt khi lỗi liên tục. Khách đòi gặp người / phàn nàn lặp lại: chuyển `human`, bot ngừng tự gửi cho ticket đó.

### 3.4 Phương án phản hồi

- Nguồn: file trong thư mục project (md, txt, pdf, docx), file tải lên, link, câu mẫu nhập tay.
- Đồng bộ bằng script theo hash/ETag, không tốn token; link theo lịch.
- `support_kb_search(query, case_type)`: top-k đoạn + nguồn (BM25 qua FTS5). Embedding chỉ thêm khi FTS không đủ.
- "Thêm vào phương án" từ một câu trả lời hay trong Hộp thư.

### 3.5 Sự kiện và hook

Sự kiện: `ticket.created`, `ticket.message`, `ticket.classified`, `ticket.awaiting_approval`, `ticket.replied`, `ticket.escalated`, `ticket.status_changed`, `ticket.sla_breached`, `ticket.closed`.

Payload: `{event, ticket{id, status, case_type, source, external_thread, customer}, message{body, by, mode}, link}`.

1. **Webhook ra ngoài**: POST JSON, ký HMAC-SHA256 (`X-Office-Signature`), thử lại 3 lần có backoff, mỗi lần gửi ghi `support_events(hook)`.
2. **Tự động hóa nguồn `support`**: lọc theo sự kiện + case; hành động `script` (0 token) hoặc `chat`/`workflow`.

### 3.6 Công cụ agent và giao diện

- Tool (chỉ có khi gói bật, prompt chỉ nhắc khi bật, ADR-163): `support_ticket_get`, `support_tickets_list`, `support_kb_search`, `support_reply` (theo mode: gửi hoặc thẻ duyệt), `support_note`, `support_set`, `support_customer_upsert`. Mở cả qua cổng MCP chung.
- Menu **Khách hàng**: Hộp thư (3 cột, badge SLA), Khách hàng, Phương án, Cài đặt (nguồn: bot/Crisp/form/API kèm URL nhận + token; loại case; hooks + nhật ký gửi).

### 3.7 Ranh giới module: gói tắt thì không ảnh hưởng

Toàn bộ code ở `internal/support/` (và `internal/support/sources/{crisp,form,api}`), dashboard ở `dashboard/app/pages/projects/[id]/support/*` + `components/support/*`. Lõi chỉ thêm **các điểm cắm nhỏ, mặc định rỗng**:

| Điểm cắm | Ở lõi | Gói tắt / chưa cấu hình |
|---|---|---|
| Đăng ký gói | `features.Packs` thêm `support` với **`DefaultOn: false`** (hiện thiếu key = bật, `features.go:67`) | project cũ không có gói này |
| Tin bot | `channels.Manager` thêm `Tap` (interface, nil mặc định), gọi trong `handle` sau kiểm danh sách được phép, trước chọn quy tắc (`manager.go:~290`). Gói nhận tin **chỉ khi** gói bật **và** kênh đó được gán làm nguồn `bot` | `Tap` trả "không nhận" → luồng quy tắc ADR-049 y nguyên |
| Route API | `support.Mount(mux, deps)` gọi một lần trong `internal/api`; middleware trả 404 khi gói tắt | 404, không chạm handler khác |
| Tool agent | `officetools` nhận thêm một danh sách provider; `support` chỉ trả tool khi gói bật cho project | không có tool, prompt không nhắc |
| Nguồn tự động hóa | `trigger` đổi danh sách nguồn cứng (`spec.go:40`) thành registry; `support` đăng ký nguồn `support` | không có nguồn mới trong form tự động hóa |
| Menu dashboard | `features` trả `support` → layout hiện menu (như `hasOps`) | không hiện |
| Migration | các bảng `support_*` | bảng rỗng, không ai truy vấn |

Phụ thuộc chỉ một chiều: `support` → lõi (`storage`, `chat`, `trigger`, `channels`, `attach`, `secrets`); lõi không import `support`, chỉ biết interface. Có test: gói tắt thì tin bot đi đúng quy tắc cũ, API support trả 404, danh sách tool không đổi.

### 3.8 Về sau: API tạo chat tổng quát

`support.Inbound` được thiết kế để nhấc lên lõi sau này thành **nguồn chat** chung:

- `POST /api/projects/{id}/chats/inbound` (token theo nguồn): tạo hoặc nối conversation theo `external_thread`, chọn agent theo quy tắc như bot (ADR-049), trả `{conversation_id}`; câu trả lời qua webhook hoặc polling.
- Khi đó `support` chỉ còn phần ticket/case/phương án đặt lên trên nguồn chat chung; kênh Crisp/form/API chuyển xuống lõi, gói gọi qua cùng interface. Không làm lúc này: chờ gói chạy thật.

## 4. Lộ trình

| Bước | Việc | Kiểm chứng |
|---|---|---|
| 0 | Không code: mẫu agent CSKH + skill "trả lời từ thư mục docs" + quy tắc bot | mốc chất lượng trên 1 bot thật |
| 1 | Khung module: `DefaultOn`, điểm cắm §3.7, migration, `internal/support` rỗng | `go test ./...`; test "gói tắt không đổi luồng" |
| 1b | Lõi Bảng dữ liệu (§7): collections, tool `data_*`, CLI/REST | `go test ./...` |
| 2 | Nguồn + ticket: bot (Tap), Crisp (vào/ra), form/API (vào), lịch sử ticket | 1 ticket thật mỗi kênh, không nhận trùng |
| 3 | Hộp thư (lịch sử, trả lời tay, ghi chú), cài đặt nguồn | dùng thật 1 tuần |
| 4 | Phương án + `support_kb_search` + nháp AI có nguồn | 50 câu thật, ≥ 80% đúng đoạn |
| 5 | Loại case + mode `auto`/`approve`/`human` + thẻ duyệt | tỉ lệ sửa nháp < 10% mới bật `auto` |
| 6 | Sự kiện: webhook HMAC + nguồn tự động hóa `support`; form trả lời qua hook | 1 hook thật nhận đủ sự kiện, thử lại khi lỗi |
| 7 | Khách hàng, SLA, báo cáo tuần | báo cáo đầu tiên |

## 5. Không làm (lúc này)

CRM đầy đủ, widget chat nhúng web, nhiều người dùng/ca trực, embedding, Zalo/Facebook (chỉ qua API khi có người dùng thật), nhấc nguồn chat lên lõi (§3.8).

## 6. Đã chốt (20/10): làm cả ba

1. **Mode case**: có đủ `auto`/`approve`/`human`; mặc định của case mới là cài đặt của gói (sẵn `approve`), đổi được theo project và theo từng case.
2. **Lõi "Bảng dữ liệu" (collections) thiết kế cùng** (§7): `support_customers` và `support_playbooks` là collection do gói khai báo, không phải bảng riêng; ticket/sự kiện/nguồn/hook vẫn là bảng `support_*` vì gắn chặt với bot, thẻ duyệt và trigger.
3. **Form trả lời khách cả hai cách**: `reply_via` = `hook` (hệ thống người dùng tự gửi) hoặc `email` (office gửi: SMTP lưu trong secrets box, hoặc một MCP email đã kết nối); nguồn chọn một.

## 7. Lõi "Bảng dữ liệu" (collections)

Lưu dữ liệu có cấu trúc trong DB của office mà không phải sửa code; gói nào cũng dùng được, không thuộc riêng `support`.

- **Bảng**: `collections` (project_id, key, name, schema JSON, pack), `records` (collection_id, data JSON, created_by, thời gian), `record_history` (dữ liệu cũ, ai sửa, lúc nào). Trường trong schema: `name`, `type` (text/number/date/enum/tags/`ref:<collection>`/`conversation`/`file`/`url`), `required`, `unique`, `indexed`.
- **Truy vấn**: `json_extract`; trường `indexed` thì office tạo expression index; trường text vào FTS5 (cách tìm không dấu như `messages`).
- **Tool chung**: `data_collections`, `data_query` (lọc, sắp xếp, FTS, phân trang), `data_get` (kèm bản ghi `ref`), `data_upsert` (chống trùng theo `unique`), `data_delete` (qua thẻ duyệt). Tạo/sửa schema qua `propose_change` loại `collection`. Quyền đọc/ghi tách theo hành động.
- **Script**: `office data query|upsert` (CLI) và REST, để tự động hóa `action=script` ghi không tốn token.
- **Gói khai báo**: gói mang manifest các collection của nó (`support`: `customers`, `playbooks`); bật gói thì tạo collection, tắt gói thì ẩn, không xóa dữ liệu.
- **Giới hạn**: số bản ghi tối đa mỗi project, tính vào backup của office. Không làm màn hình nhập liệu nghiệp vụ (tránh trôi thành ERP, đã bỏ ngày 19/10).
- **Thứ tự**: làm trước bước 2 của §4 (thêm bước 1b: collections + tool + CLI, kiểm bằng `go test ./...`).

## 8. Kết quả review (20/10)

Ba agent và quy trình review 2 bên: **duyệt kèm sửa**. Đã ghi thành ADR-171. Thay đổi so với bản trên:

1. Cả ba đề nghị hoãn lõi "Bảng dữ liệu"; người dùng giữ quyết định §6.2: **làm cùng** (ADR-172, bước 1b đã xong).
2. Chống trùng ở DB, transaction cho ticket + sự kiện, outbox cho gửi đi (`sent` chỉ khi gửi được, `send_failed` khi lỗi).
3. Cổng nhận: token, giới hạn kích thước, tốc độ theo nguồn, trước khi xếp việc. Việc của gói chạy như nguồn không tin cậy.
4. Thẻ duyệt gắn bản nháp cố định; người trả lời trực tiếp hoặc khách nhắn mới thì hủy nháp đang chờ. Đòi gặp người luôn dừng bot.
5. Hộp thư duyệt ngay tại chỗ (sửa, gửi 1 chạm, xem đoạn nguồn và độ tự tin); trạng thái gửi (đã gửi, lỗi hook, lỗi email) trên dòng lịch sử.
6. Thứ tự: khung → ticket + nguồn + gửi đi → Hộp thư → phương án + nháp + duyệt → `auto` → hook → khách hàng/SLA/báo cáo; email cho form sau hook.

Bước 1b (Bảng dữ liệu, ADR-172) đã làm: collections, tool `data_*`, API, `propose_change` loại `collection`; gói `support` tạo sẵn `customers`, `playbooks`.

Bước 1 (khung) đã làm: `features` gói `support` mặc định tắt, `channels.SetTap`, `officetools.AddProvider`, `api.Config.Packs`, `internal/support`, bật/tắt ở dashboard.

## 9. API vào/ra (ADR-173, đã làm 20/10)

1. Tạo tự động hóa trả lời (nguồn webhook, hành động chat với agent CSKH hoặc quy trình), prompt ví dụ: "Tra collection playbooks (data_query) theo nội dung khách, soạn câu trả lời ngắn, lịch sự, kèm đối sách".
2. Bật gói, tạo nguồn: `POST /api/projects/{id}/support/sources {"name":"Review web","kind":"api","automation_id":"…","mode":"auto"}`, lưu `token` trả về.
3. Gửi review:

```bash
curl -X POST "$OFFICE/hooks/support/$SOURCE" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"external_id":"review-123","thread":"order-77","text":"Áo giao sai size","customer":{"name":"An","email":"an@x.vn"},"meta":{"rating":1},"wait":60}'
# → {"status":"answered","ticket_id":"tkt_…","message_id":"tev_…","reply":{"text":"…"},"poll":"/hooks/support/…/messages/…"}
```

`mode: approve` thì trả `waiting_approval`; người duyệt (kèm sửa) rồi bên gọi lấy câu trả lời qua `poll`.

## 10. Hoàn thiện (ADR-174, 21/10)

Đã làm hết lộ trình §4: nguồn bot/Crisp/form (+ email), outbox có thử lại, hook ký HMAC hoặc chạy tự động hóa, loại case + chế độ + từ đòi gặp người, SLA, báo cáo, tool agent (`support_kb_search`, `support_ticket_get|list`, `support_note`, `support_set`), đồng bộ phương án từ file/link, `office data` CLI, `data_delete` qua thẻ duyệt. Giao diện: Hộp thư, Nguồn, Dữ liệu, Báo cáo, Cài đặt.
