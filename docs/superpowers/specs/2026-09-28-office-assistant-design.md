# Trợ lý office, bộ công cụ chung và nhật ký thay đổi (ADR-042 giai đoạn 2)

Ngày: 2026-09-28 · Trạng thái: chờ duyệt

## 1. Mục tiêu

Người dùng cần một **trợ lý của toàn office**. Trợ lý này:
- biết việc thuộc project nào để chuyển tới đó;
- làm thống kê và báo cáo;
- cài đặt mọi thứ có thể cấu hình (tự động hóa, quyền, agent, mô hình, kết nối AI, giám sát, tiến trình, ngân sách);
- thao tác vận hành.

Mọi thay đổi đều qua **thẻ xác nhận**.

Yêu cầu bổ sung (2026-09-28): về sau phải tra được **ai đổi gì**:
- người hay agent nào đổi, người nào duyệt;
- đổi trong chat, job hay việc nào, qua kênh nào (giao diện, chat, trợ lý, CLI, tự động hóa);
- giá trị trước và sau khi đổi;
- thống kê được theo các chiều trên.

**Tiêu chí thành công**
- Ở trang chủ, nhắn "tạo trigger Discord cho repo A": trợ lý hiện thẻ xác nhận cho đúng project A, bấm Duyệt là tạo xong.
- Hỏi "tuần này tốn bao nhiêu, project nào tốn nhất": trả lời bằng số liệu thật từ `jobs`/`runs`.
- Mọi thay đổi cấu hình (từ UI, agent, trợ lý hay CLI) đều có đúng một dòng nhật ký. Dòng đó cho biết người/agent, người duyệt, chat/job/việc nguồn và diff trước/sau.
- Claude Code CLI của người dùng gọi được cùng bộ công cụ bằng token cá nhân.

**Ngoài phạm vi:** sửa code từ trợ lý office. Việc cần code thì trợ lý chuyển sang Chat của project.

## 2. Hiện trạng (tóm tắt)

- `internal/officetools` + `/mcp`:
  - Công cụ đọc: ops, logs, monitor, git.
  - Công cụ đề xuất: `run_command`, `propose_action`, `propose_automation`.
  - Chỉ dùng được trong một project, bởi agent đang chạy.
- `actions` là luồng duyệt:
  - Lưu `proposed_by`, `decided_by`, `conversation_id`, `task_id`, `run_ref`.
  - `project_id` bắt buộc có.
- `audit_log(actor, action, target, detail, at)`:
  - Khoảng 55 thao tác API ghi vào đây, nhưng actor luôn là `human:<email>`.
  - Chưa có project, nguồn (chat/job/việc), agent đề xuất, trước/sau.
  - Thay đổi do agent đề xuất rồi người duyệt chỉ ghi người duyệt.
- `org_revisions` giữ lịch sử của mô hình tổ chức và agent (có khôi phục). Bảng này vẫn giữ.

## 3. Thiết kế

### 3.1 Nhật ký thay đổi (làm trước, các phần sau dựa vào nó)

**Bảng.** Mở rộng `audit_log` (migration 00024), thêm các cột:

| Cột | Ý nghĩa |
|---|---|
| `actor_kind` | `human` \| `agent` \| `automation` \| `system` |
| `actor_id` | user id, agent id hoặc automation id |
| `actor_name` | tên hiển thị lúc ghi (vẫn còn khi bị xóa) |
| `approved_by` | người duyệt, khi thay đổi đi qua thẻ xác nhận |
| `via` | `ui` \| `chat` \| `task` \| `assistant` \| `mcp` \| `automation` \| `api` |
| `project_id` | project bị ảnh hưởng; rỗng nếu thuộc cấp office |
| `conversation_id`, `job_id`, `task_id`, `action_id` | nguồn của thay đổi |
| `resource`, `resource_id` | ví dụ `automation`, `aut_123` |
| `before`, `after` | JSON, đã che bí mật |
| `ok` | lần thử thất bại cũng được ghi |

- `actor` và `target` cũ vẫn giữ để tương thích.
- Index trên `(project_id, at)`, `(resource, resource_id, at)`, `(actor_kind, actor_id, at)`, `(conversation_id)`, `(job_id)`.

**Một cổng ghi duy nhất:** `internal/audit`.
- `audit.Record(ctx, Change{…})` lấy **người thực hiện** từ `ctx`:
  - Request UI mang `human` với `via=ui`.
  - Khi agent chạy, `actions.Scope` mang `agent`, conversation/task/job/run.
  - Khi duyệt thẻ: `actor` là agent đề xuất, `approved_by` là người bấm Duyệt.
  - Tự động hóa mang `automation` và `job_id`.
- `auditAction` hiện tại trong API chuyển sang gọi `audit.Record`, để ~55 chỗ đang ghi tự có thêm project và trước/sau. Handler cập nhật đọc bản cũ trước khi ghi.
- **Che bí mật:** các trường có tên `api_key`, `token`, `secret`, `password` và các trường đã đánh dấu mã hóa được ghi thành `"***"`. Nhật ký chỉ cho biết là *đã đổi*.
- **Chỉ thêm, không sửa, không xóa**, như `AuditRepo` hiện nay. Không tự dọn.

**Xem và thống kê**
- `GET /api/audit` có lọc: project, resource, actor_kind, actor_id, via, conversation_id, job_id, from/to; phân trang theo `before`.
- `GET /api/audit/stats` đếm theo ngày, người/agent, resource, via.
- Trang **Nhật ký** ở cấp office, thay trang audit hiện tại. Có bộ lọc trên một hàng. Mỗi dòng có:
  - ai (có huy hiệu người/agent/tự động hóa) và người duyệt;
  - đổi gì;
  - liên kết tới chat/job/việc nguồn;
  - diff trước/sau khi mở ra.
- Tab **Nhật ký** trong project dùng cùng component, lọc sẵn theo project.
- Trang chi tiết (agent, tự động hóa, giám sát…) có mục "Lịch sử thay đổi", đọc cùng bảng theo `resource_id`.
- Công cụ `audit_query` và `audit_stats` cho trợ lý (mục 3.2).

### 3.2 Bộ công cụ chung (mở rộng `officetools`, không làm bộ mới)

Toolbox có thêm **phạm vi**: `project` (như hiện nay) hoặc `office`. Ở phạm vi office, mọi công cụ nhận thêm tham số `project`, là id hoặc tên.

**Công cụ cấu hình chung**, dựa trên một registry `internal/config`:

| Công cụ | Làm gì |
|---|---|
| `describe(resource?)` | Không có tham số: liệt kê các loại cài đặt. Có tham số: JSON schema cùng mô tả trường và giá trị hợp lệ. |
| `list(resource, project?)` | Danh sách rút gọn. |
| `get(resource, id)` | Bản đầy đủ, đã che bí mật. |
| `propose_change(resource, op, id?, patch, reason)` | `op` là create, update hoặc delete. Tạo thẻ xác nhận `config_change`, lưu sẵn trước/sau. |

- Các resource ở v1: `project`, `policy` (danh mục lệnh), `agent` (kèm quyền), `automation`, `monitor`, `process`, `provider`, `org_model`, `usage_settings`.
- Mỗi resource trong registry khai báo: schema, List, Get, **Validate**, **Apply**, và danh sách trường bí mật.
- **Một đường ghi:**
  - Handler API của resource đó chuyển sang gọi cùng Apply của registry. Kiểm tra dữ liệu và ghi nhật ký vì thế chỉ có một chỗ.
  - `propose_automation` hiện có trở thành một trường hợp của `propose_change`. Tên cũ vẫn giữ làm bí danh.

**Công cụ đọc số liệu:** `jobs_query`, `usage_summary` (chi phí theo project/agent/mô hình/ngày), `audit_query`, `audit_stats`.

**Công cụ điều phối** (luôn qua thẻ xác nhận, vì tốn token hoặc gây tác động):
- `start_task(project, title, prompt, agent?)`
- `run_automation(id)`
- `handoff(project, message)`: mở Chat của project với nội dung soạn sẵn, dùng khi việc cần code.

**Thẻ xác nhận**
- Là action kiểu `config_change`, `start_task`, `run_automation`.
- Hiện diff trước/sau theo từng trường.
- `actions.project_id` được phép rỗng (migration dựng lại bảng) cho thay đổi cấp office như provider.
- Duyệt thì chạy `Apply` với `ctx` mang agent đề xuất và người duyệt, nên nhật ký ghi đúng cả hai.
- Nếu dữ liệu đã đổi kể từ lúc đề xuất (`before` không còn khớp) thì từ chối và báo "đã có thay đổi mới, hãy đề xuất lại".

**API key của kết nối AI (cách 1, đã chốt)**
- `propose_change(provider, create|update)` không bao giờ nhận key.
- Thẻ xác nhận có ô để người dùng tự dán key. Key đi thẳng từ trình duyệt vào API, AI không bao giờ thấy.

**Quyền**
- Chỉ admin mới duyệt được thay đổi cấp office và provider.
- Thành viên được dùng các công cụ đọc. `propose_change` của thành viên vẫn tạo thẻ, nhưng chỉ admin duyệt được.

### 3.3 Trợ lý office

- Là một cuộc chat có `purpose='office'`, `project_id` rỗng (dựng lại bảng `conversations` để cho phép rỗng).
  - Mỗi người có danh sách chat trợ lý riêng.
  - Loại chat này không hiện trong Chat của project nào.
- **Chạy bằng** kết nối AI mặc định và mô hình cân bằng, như đã chốt. Cài được trong phần cài đặt office.
- **Không sửa code:**
  - Claude Code chạy ở `.office/assistant/` (thư mục trống).
  - Chỉ có công cụ office qua MCP ở phạm vi office, cộng Read/Grep, và không có Edit/Write/Bash.
  - Kết nối kiểu API cũng dùng cùng Toolbox.
- **System prompt:**
  - Có danh sách project (id, tên, mô tả ngắn) và các loại resource.
  - Quy tắc: hỏi lại khi không rõ project; việc cần code thì dùng `handoff`; mọi thay đổi đi qua `propose_change`.
  - Ngữ cảnh trang được gửi kèm như giai đoạn 1.
- **Giao diện:**
  - Chat ở góc có **chip phạm vi**: `Project: A` hoặc `Office`.
  - Ngoài project (trang chủ, Việc, Jobs, cài đặt) mặc định là Office.
  - Trong project mặc định là project đó, đổi được sang Office.
  - Trang `/assistant` là bản đầy đủ, có danh sách chat.
- Mỗi lượt trả lời là một job `chat_turn` với `origin=user`, nên chi phí đã nằm trong thống kê jobs. Trong thống kê, `project_id` rỗng hiện là "Office".

### 3.4 MCP cho Claude Code CLI

- **Token cá nhân:** tạo ở trang Tài khoản.
  - Hiện một lần, lưu dạng SHA-256, thu hồi được.
  - Có hạn dùng và thời điểm dùng gần nhất.
- `/mcp` nhận `Authorization: Bearer <token>`:
  - Phạm vi office, quyền theo vai trò của người sở hữu token.
  - `via=mcp`, `actor` là người sở hữu.
- Token có chế độ:
  - **đề xuất** (mặc định): `propose_change` tạo thẻ chờ duyệt, hiện ở hộp "Chờ duyệt" trên dashboard;
  - **áp dụng ngay**: chỉ admin bật được; lúc này hộp xác nhận quyền của Claude Code là bước xác nhận.
- Trang Tài khoản có đoạn lệnh `claude mcp add --transport http agent-office http://…/mcp --header "Authorization: Bearer …"` để chép.

### 3.5 Lỗi và giới hạn

- Công cụ trả lỗi rõ ràng: project không có, resource không có, trường sai. AI tự sửa và đề xuất lại.
- `propose_change` kiểm tra bằng Validate **ngay lúc đề xuất**, nên thẻ không bao giờ chứa bản nháp hỏng.
- Giới hạn: tối đa 20 thẻ đang chờ trên mỗi cuộc chat; patch tối đa 64KB.
- Nếu ghi nhật ký thất bại thì thay đổi đó thất bại theo, nhờ cùng transaction SQLite. Không có thay đổi nào lọt khỏi nhật ký.

## 4. Thứ tự làm (mỗi phần là một plan)

1. **Nhật ký thay đổi**: migration 00024, `internal/audit`, chuyển `auditAction` sang, trước/sau cho các handler cập nhật, API lọc/thống kê, trang Nhật ký và tab trong project.
2. **Registry cấu hình + công cụ chung**: `describe`, `list`, `get`, `propose_change`, thẻ diff, cổng key provider; handler API dùng chung Apply.
3. **Trợ lý office**: chat `purpose=office`, Toolbox phạm vi office, công cụ số liệu và điều phối, chip phạm vi, trang `/assistant`.
4. **MCP cho CLI**: token cá nhân, chế độ đề xuất/áp dụng.

## 5. Kiểm thử

- `audit`: mỗi kiểu người thực hiện ghi đúng các cột. Bí mật bị che. Thay đổi thất bại được ghi với `ok=0`. Lỗi ghi nhật ký thì hủy thay đổi.
- Duyệt thẻ do agent đề xuất: `actor_kind=agent`, `approved_by` là người duyệt, có `conversation_id`/`job_id`.
- Registry: Validate từ chối patch sai. Apply qua API và qua thẻ cho ra cùng một kết quả (test dùng chung). `before` lệch thì bị từ chối.
- Phạm vi: agent ở phạm vi project không đọc hay đổi được project khác. Thành viên không duyệt được thẻ cấp office.
- Provider: `propose_change` có trường key thì bị từ chối.
- MCP: token sai hoặc đã thu hồi trả 401. Chế độ đề xuất không áp dụng ngay.
- Chạy thật: kịch bản "tạo trigger Discord cho repo A" từ trang chủ, rồi xem dòng nhật ký có liên kết về chat trợ lý.
