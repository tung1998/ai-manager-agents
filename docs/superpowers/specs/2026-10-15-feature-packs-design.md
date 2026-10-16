# Gói tính năng: ẩn những gì không dùng

## Mục tiêu

- Office sẽ dùng cho cả người không làm code (ADR-090). Với họ, Vận hành, Giám sát, git, lệnh build/test chỉ làm rối menu, tab và widget.
- **Gói tính năng** gom nhiều tính năng con, giống level quyền gom nhiều quyền con (`internal/perm/caps.go`). Bật gói thì các tính năng con bật; tắt lẻ được từng cái.
- **Lõi luôn có:** chat, agent, quy trình, tự động hóa, Burn, trí nhớ, duyệt thay đổi (worktree + diff), Skills & MCP.
- **Nhiều gói bật cùng lúc.** Gói đầu tiên là **Develop**; về sau thêm gói Nội dung, Khách hàng… chỉ là thêm vài dòng trong registry.
- Không phải lớp quyền: quyền vẫn do level và cap của agent quyết. Tính năng tắt chỉ **ẩn khỏi giao diện**, dữ liệu giữ nguyên.

## Gói Develop

| Tính năng | Ẩn khi tắt (chỉ phần giao diện; công cụ/đề xuất của agent không đổi, xem mục dưới) |
|---|---|
| `git` | GitBar, menu nhánh, review PR, widget git; công cụ `git_status/git_diff/git_log`; đề xuất `git_commit/git_branch/git_push` |
| `ops` | tab Vận hành (tiến trình, container, env), widget process/container/consoles; công cụ `ops_overview/process_logs/container_logs`; đề xuất `run/restart/stop_process`, `start/restart/stop_container` |
| `monitor` | mục Giám sát, sự cố, widget monitor/incidents, badge sự cố; công cụ `monitor_detail` |
| `checks` | mục Gói lệnh và Quick check trong tab Quyền (đường dẫn cấm, project liên kết vẫn hiện vì thuộc lõi), quick check sau khi sửa; công cụ `run_command`, đề xuất `run_command` |

- **Git chỉ ẩn giao diện.** Office vẫn dùng git ngầm cho worktree và diff khi thư mục là repo; duyệt thay đổi thuộc lõi, không đổi.
- Project không có thư mục: `git` tự tắt. `checks` vẫn giữ vì helper toàn máy vẫn chạy lệnh trong thư mục home.
- Tab Vận hành ẩn khi cả `ops` và `monitor` cùng tắt.

## Dữ liệu

Registry tĩnh trong `internal/features`:

```go
type Feature struct{ ID, Pack string }
type Pack struct{ ID string; Features []string }
var Packs = []Pack{{ID: "develop", Features: []string{"git", "ops", "monitor", "checks"}}}
```

Lưu trong bảng `settings` (như `policy:<projectID>`), không cần migration:

**Chỉ cài đặt theo project, không có cài đặt chung.** Key `features:<projectID>`, dạng `{"packs": {"develop": false}, "off": ["monitor"]}`.

**Tính tập hiệu lực** `features.Effective(cfg, hasPath) []string`:

1. Gói bật (thiếu trong `packs` = bật), lấy hết tính năng của gói.
2. Bỏ các tính năng trong `off`.
3. Không có thư mục: bỏ `git`.

Thiếu key = Develop bật, nên mọi project hiện có giữ nguyên.

Lý do tắt (để giao diện và API giải thích): `project`, `no_folder`.

## Chỉ ẩn giao diện (sửa 15/10)

Người dùng chốt: tắt tính năng chỉ để đỡ rối giao diện. Agent giữ nguyên công cụ, quyền, `run_command` và đề xuất (họ vẫn có thể chạy tool có sẵn trong repo); API không chặn. Backend chỉ: lưu cấu hình, trả tập hiệu lực trong payload project, và không tính sự cố của phần đã tắt vào badge. Prompt hệ thống bỏ câu hướng dẫn về công cụ của tính năng đã tắt (`internal/chat/devtools.go`, `chat/office-tools`, `chat/diff`, `chat/edit-worktree`): agent không tự nhắc commit/push, chạy build/test; người dùng yêu cầu thì vẫn làm.

**Đang chạy thì giữ nguyên:** monitor, tiến trình tự chạy lại vẫn chạy. Lúc bấm tắt `ops`/`monitor`, nếu còn thứ đang chạy, giao diện hỏi lại.

## API

- `GET /api/features`: registry (gói, tính năng, nhãn).
- `GET /api/projects/{id}/features`: ngoại lệ của project + tập hiệu lực + lý do của từng tính năng tắt. `PUT` cho người sửa được project.
- Payload project (`GET /api/projects`, `/api/projects/{id}`) kèm `features: string[]` (tập hiệu lực).
- Thay đổi ghi Nhật ký (ADR-043) và đẩy sự kiện SSE để dashboard cập nhật ngay.

## Dashboard

- `useFeatures(projectId)`: `has('ops')`.
- Chỗ ẩn:
  - menu project `layouts/default.vue:73-96`, tab `pages/projects/[id]/index.vue:18`;
  - mục trong `OpsPanel.vue:34`; mục Gói lệnh và Quick check trong `PolicyPanel.vue`;
  - `GitBar`, `BranchMenu`, review PR trong chat;
  - widget: thêm `feature?: string` vào định nghĩa trong `utils/watchWidgets.ts`; `WatchPicker` không đưa ra widget tắt, box đã đặt hiện "Tính năng đã tắt". Widget gắn project nào thì theo project đó; trong picker chỉ ẩn loại widget khi không project nào bật tính năng tương ứng.
  - Link cũ tới tab đã ẩn quay về Chat (như `log`, `files` với người không phải admin).
- **Cài đặt:** mỗi gói là một thẻ có công tắc lớn, mở ra là công tắc từng tính năng (kiểu `AgentPermEditor.vue`).
  - Đặt ở tab Thông tin của project. Không thư mục: `git` hiện xám kèm "Project không có thư mục".
- i18n vi/en cho nhãn gói, tính năng, lý do.

## Ngoài phạm vi

- Preset nhanh ("Lập trình", "Không code") khi tạo project: làm sau khi có gói thứ hai.
- Gói Nội dung, Khách hàng; ẩn Skills & MCP cho người mới.
- Chép thư mục không phải git để duyệt thay đổi.

## Kiểm thử

- Go:
  - `features.Effective`: tắt gói, tắt lẻ, không thư mục, thiếu key = Develop bật.
  - API lưu/đọc cấu hình, payload project kèm `features`; incidents bỏ sự cố của phần đã tắt.
- Dashboard: typecheck, `check-i18n`; kiểm UI thật trên demo: tắt Develop ở một project, chỉ tắt `ops` ở project khác, xem menu, tab, widget, thẻ cài đặt (desktop và `--mobile`).
