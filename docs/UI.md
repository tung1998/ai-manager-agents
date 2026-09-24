# Dashboard

Nuxt 4 + @nuxt/ui, app riêng trong `dashboard/` (ADR-010). Gọi REST + SSE của `office` server. Ngôn ngữ vi/en.

## Màn hình

| Route | Màn hình | Nội dung chính | Hành động |
|---|---|---|---|
| `/` | Sơ đồ tổ chức | Cây Director → Manager → Worker. Mỗi node: trạng thái (idle/running/disabled), mode, next check, cost hôm nay | Kéo-thả worker vào manager để gán, click mở Agent detail |
| `/agents/:id` | Agent detail | Tab **Cấu hình** (runtime, model, fallback, heartbeat), **Rules** (instructions, tools, strict mode, permission, timeout, retry, cost cap, notify, debounce, chain), **Prompt** (role template + override), **Memory** (3 layer, lịch sử version, diff), **Runs** (danh sách + log stream) | Lưu cấu hình (preview diff), chạy ngay, ép compaction, bật/tắt |
| `/blackboard` | Blackboard | Feed thread dạng kênh chat nội bộ. Finding có màu theo loại, badge confidence, chip evidence mở được worker output | Lọc theo agent/loại/severity, trả lời thread (người đặt câu hỏi) |
| `/incidents` | Incidents | Danh sách + chi tiết: timeline debate theo round, kết luận, confidence, phương án, rủi ro, evidence | **Duyệt / Từ chối** từng option có side effect, ghi chú |
| `/costs` | Chi phí | Token và USD theo agent/ngày, theo incident, so với budget, dự báo cuối ngày | Chỉnh budget (ghi config) |
| `/setup` | Setup wizard | Giao diện cho `init`: kết quả quét, runtime, đề xuất cơ cấu, diff | Tạo / Chỉnh / Hủy |

Thêm: `/ask` (ô hỏi Director, stream kết quả) và badge "chờ duyệt" trên header.

## Luồng người dùng

**Lần đầu (setup).**
1. Mở `/setup`. UI gọi `POST /api/init/scan`.
2. Xem kết quả quét và runtime khả dụng. Nhập budget.
3. `POST /api/init/propose` trả đề xuất kèm `reason` và ước tính chi phí.
4. Chỉnh trực tiếp trên form. Bấm Tạo gọi `PUT /api/config` với `If-Match` là hash config.
5. Chuyển sang `/`, thấy org chart.

**Hằng ngày.**
1. Nhận Discord "incident mới" có link `/incidents/:id`.
2. Đọc kết luận, mở evidence, xem debate.
3. Duyệt hoặc từ chối option. Hành động được thực thi và kết quả hiện realtime.

**Tinh chỉnh agent.**
1. Từ org chart mở `/agents/tech-lead`.
2. Đổi model hoặc sửa prompt override. UI hiện diff config trước khi lưu.
3. Bấm "Chạy ngay" để thử, xem log stream.

## API

Base `/api`. Auth bằng cookie phiên `office_session` sau khi đăng nhập tài khoản (ADR-014). Role `admin` quản lý tài khoản và audit, `member` chỉ xem. Passkey để sau.

### Kết nối AI, mô hình, repo (đã làm)
| Method | Path | Quyền | Mô tả |
|---|---|---|---|
| GET | `/api/system` | đăng nhập | Chế độ cài đặt, thư mục dữ liệu |
| GET | `/api/provider-kinds` | đăng nhập | Các loại kết nối và gợi ý |
| GET | `/api/providers` | đăng nhập | Danh sách, không có key |
| POST/PATCH/DELETE | `/api/providers[/:id]` | admin | `api_key` bỏ qua = giữ, `""` = xóa |
| POST | `/api/providers/:id/default` | admin | Đặt mặc định |
| POST | `/api/providers/:id/test` | admin | `{prompt?, model?}` |
| GET | `/api/templates` | đăng nhập | Thư viện mẫu |
| POST | `/api/templates` | admin | `{source_id, key, name}` nhân bản, hoặc `{template}` import |
| POST | `/api/templates/:key/reset` | admin | Khôi phục mẫu có sẵn |
| GET/PATCH/DELETE | `/api/org-models/:id` | đăng nhập/admin | Mô hình kèm agent; sửa tên, loại, governance |
| GET | `/api/org-models/:id/export` | đăng nhập | JSON Template |
| POST | `/api/org-models/:id/agents` | admin | Thêm agent |
| PATCH/DELETE | `/api/agents/:id` | admin | Sửa/xóa agent; lỗi cấu trúc trả `problems[]` |
| GET/POST | `/api/projects` | đăng nhập/admin | `{path?, name?, template_id?}`; bỏ trống path = helper toàn máy (bắt buộc name) |
| GET/PATCH/DELETE | `/api/projects/:id` | đăng nhập/admin | Xóa chỉ bỏ quản lý, không đụng file |
| POST | `/api/projects/:id/model` | admin | `{template_id, replace}` |
| GET | `/api/fs/dirs?path=&hidden=1` | admin | Thư mục con cho cây chọn folder, kèm shortcut |
| GET | `/api/org-models/:id/revisions` | đăng nhập | Lịch sử chỉnh sửa |
| GET | `/api/revisions/:id` | đăng nhập | Snapshot |
| POST | `/api/revisions/:id/restore` | admin | Khôi phục (tạo snapshot hiện tại trước) |
| GET | `/api/transfer/export` | admin | Tải bundle config |
| POST | `/api/transfer/import` | admin | `{bundle, dry_run}` → danh sách thay đổi |
| POST | `/api/transfer/backup` | admin | Tạo backup trên máy chạy office |
| POST | `/api/projects/:id/setup/scan` | admin | Quét tĩnh, trả summary |
| POST | `/api/projects/:id/setup/propose` | admin | `{goal?}` → đề xuất AI; `412 no_provider` khi chưa có kết nối |
| POST | `/api/projects/:id/setup/build` | admin | `{template_key, changes}` → template + problems (xem trước) |
| POST | `/api/projects/:id/setup/apply` | admin | `{template_key, changes, description}` → cài mô hình cho project |

### Auth + tài khoản (đã làm)
| Method | Path | Quyền | Mô tả |
|---|---|---|---|
| GET | `/api/health` | public | Trạng thái + version |
| GET | `/api/auth/status` | public | `{has_users}` để trang login hướng dẫn tạo admin đầu tiên |
| POST | `/api/auth/login` | public | `{email, password}`, đặt cookie |
| POST | `/api/auth/logout` | public | Xóa phiên hiện tại |
| GET | `/api/auth/me` | đăng nhập | User hiện tại |
| POST | `/api/auth/password` | đăng nhập | `{old_password, new_password}`, thu hồi các phiên khác |
| GET | `/api/users` | admin | Danh sách tài khoản |
| POST | `/api/users` | admin | `{email, name, role, password}` |
| PATCH | `/api/users/:id` | admin | `{disabled}`; không tự vô hiệu hóa chính mình |
| POST | `/api/users/:id/reset-password` | admin | `{password}`, thu hồi mọi phiên |
| GET | `/api/audit?limit=` | admin | Audit log mới nhất |

### Config
| Method | Path | Mô tả |
|---|---|---|
| GET | `/api/config` | Config hiện tại + `hash` |
| PUT | `/api/config` | Ghi toàn bộ, validate schema, cần `If-Match` |
| POST | `/api/config/diff` | So config gửi lên với hiện tại |
| GET | `/api/config/history` | Danh sách bản lưu |
| GET | `/api/schema` | JSON Schema để UI dựng form |

### Org + agent
| Method | Path | Mô tả |
|---|---|---|
| GET | `/api/org` | Cây tổ chức + trạng thái |
| GET | `/api/agents/:id` | Chi tiết + thống kê |
| PATCH | `/api/agents/:id` | Sửa một phần config của agent (ghi vào file) |
| POST | `/api/agents/:id/run` | Chạy ngay |
| POST | `/api/agents/:id/enable` \| `/disable` | |
| GET | `/api/agents/:id/memory?layer=` | Memory hiện hành |
| GET | `/api/agents/:id/memory/history?layer=` | Các version |
| POST | `/api/agents/:id/memory/compact` | Ép compaction |
| GET | `/api/roles` | Danh sách role template |

### Runs
| Method | Path | Mô tả |
|---|---|---|
| GET | `/api/runs?agent=&status=&cursor=` | Danh sách |
| GET | `/api/runs/:id` | Chi tiết + events |
| GET | `/api/worker-outputs/:id` | Evidence (data + summary, raw qua `?raw=1`) |

### Blackboard
| Method | Path | Mô tả |
|---|---|---|
| GET | `/api/threads?status=&cursor=` | |
| GET | `/api/threads/:id` | Thread + findings + evidence |
| POST | `/api/threads/:id/messages` | Người đặt câu hỏi vào thread |

### Incidents + approvals
| Method | Path | Mô tả |
|---|---|---|
| GET | `/api/incidents?status=` | |
| GET | `/api/incidents/:id` | Kết luận, options, timeline |
| GET | `/api/approvals?status=pending` | |
| POST | `/api/approvals/:id/approve` \| `/reject` | Body `{note}` |

### Ask + init
| Method | Path | Mô tả |
|---|---|---|
| POST | `/api/ask` | Body `{question, managers?, budget_usd?}` trả `thread_id` |
| POST | `/api/init/scan` | Quét tĩnh |
| POST | `/api/init/propose` | Đề xuất (có gọi LLM) |

### Chi phí + trạng thái
| Method | Path | Mô tả |
|---|---|---|
| GET | `/api/costs/daily?from=&to=&group=agent` | |
| GET | `/api/costs/incidents?from=&to=` | |
| GET | `/api/status` | Giống `office status` |
| GET | `/api/doctor` | Giống `office doctor` |

### Realtime (SSE)
| Path | Event |
|---|---|
| `GET /api/stream` | `agent.status`, `run.started`, `run.finished`, `finding.created`, `incident.updated`, `approval.created`, `cost.updated` |
| `GET /api/runs/:id/stream` | `run.event` (log từng dòng) |

Mỗi event có `id` tăng dần để client nối lại bằng `Last-Event-ID`.

### Webhook (không cho UI)
| Method | Path | Mô tả |
|---|---|---|
| POST | `/hooks/:source` | Verify theo `TriggerSource`, dedupe vào `events` |

## Nguyên tắc UI/UX

- Config là nguồn sự thật: mọi form hiện diff trước khi ghi và báo xung đột nếu file đã đổi (`412`).
- Evidence luôn click được. Kết luận không có evidence không được hiển thị như kết luận.
- Hành động side effect luôn cần xác nhận hai bước và hiện rõ tool + args sẽ chạy.
- Chi phí hiển thị ở mọi nơi có run (badge `$0.02`).
