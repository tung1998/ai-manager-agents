# Gói tính năng (feature packs) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Sửa 15/10:** chỉ ẩn giao diện. Phần chặn ở backend (Task 2, 3, phần 403 của Task 4) đã bỏ; xem ADR-161.

**Goal:** Mỗi project tắt được gói Develop hoặc từng tính năng con (`git`, `ops`, `monitor`, `checks`); tính năng tắt biến khỏi menu, tab, widget và khỏi công cụ/đề xuất/quyền của agent.

**Architecture:** Registry tĩnh `internal/features` + cấu hình theo project trong bảng `settings` (`features:<projectID>`). `perm.LoadPolicyRaw` gắn tập hiệu lực vào `Policy.Features` (không lưu), nên mọi chỗ đã đọc policy (Resolve, engine) có sẵn. `officetools.ToolsFor`, `actions.Propose/Decide`, API và incidents tự đọc qua `features.For`. Dashboard đọc `features` trong payload project.

**Tech Stack:** Go (net/http ServeMux, SQLite settings), Nuxt 4 + Nuxt UI, i18n vi/en.

**Spec:** `docs/superpowers/specs/2026-10-15-feature-packs-design.md`

## Global Constraints

- Thiếu key `features:<id>` = mọi tính năng bật (project hiện có không đổi).
- Tắt chỉ ẩn/chặn: không xóa dữ liệu, không dừng tiến trình/monitor.
- Không thư mục ⇒ tắt `git` với lý do `no_folder` (`checks` vẫn giữ: helper toàn máy chạy lệnh ở home).
- Duyệt thay đổi (worktree + diff) thuộc lõi, không bị ảnh hưởng.
- Comment theo giọng code xung quanh (tiếng Anh ngắn), chuỗi người đọc tiếng Việt, UI có i18n vi/en.

## Review Focus

1. Thẻ đề xuất tạo trước khi tắt tính năng rồi mới bấm duyệt ⇒ phải bị từ chối, không chạy (test ở Task 3).
2. `ops` tắt nhưng `monitor` bật ⇒ `ops_overview`, `monitor_detail` vẫn còn; tab Vận hành chỉ còn Giám sát (Task 3, Task 6).
3. Project không có thư mục nhưng config bật hết ⇒ `git` vẫn tắt, `checks` còn (Task 1).
4. Cấu hình hỏng (JSON lỗi) ⇒ coi như bật hết, không làm sập trang (Task 1).
5. PolicyPanel lưu khi mục Gói lệnh đang ẩn ⇒ không làm mất gói lệnh/quick check đã lưu (Task 2: features không lưu trong policy; Task 6 không gửi trường mới).

---

### Task 1: `internal/features`

**Files:** Create `internal/features/features.go`, `internal/features/features_test.go`

**Interfaces — Produces:**
- `const Git, Ops, Monitor, Checks string`; `type Pack{ID string; Features []string}`; `var Packs []Pack`
- `type Config{Packs map[string]bool; Off []string}`
- `func Effective(c Config, hasPath bool) (on []string, off map[string]string)` (lý do `project` | `no_folder`)
- `func LoadConfig(ctx, st storage.Store, projectID string) Config`, `func Save(ctx, st, projectID string, c Config) error`
- `func For(ctx, st, projectID string) []string` (đọc repo để biết có path)
- `func Has(on []string, f string) bool` (nil = bật hết)
- `func ToolOn(on []string, tool string) bool`, `func ActionOn(on []string, kind string) bool`

- [ ] Test: Effective mặc định bật hết; tắt gói; `off` lẻ; không path bỏ git (lý do no_folder thắng project); ToolOn(`ops_overview`) đúng khi chỉ còn monitor; ActionOn(`git_push`) false khi git tắt; LoadConfig với JSON hỏng = Config rỗng.
- [ ] Viết code, `go test ./internal/features/`.

### Task 2: Policy + Resolve + engine (quyền, prompt, quick check)

**Files:** Modify `internal/perm/perm.go` (Policy.Features, Has, LoadPolicyRaw), `internal/perm/caps.go` (Resolve che cap), `internal/chat/engine.go` (prompt, QuickCheck), `internal/prompts/chat/office-tools.md`; tests `internal/perm/caps_test.go`.

- `Policy.Features []string \`json:"-"\``; `func (p Policy) Has(f string) bool { return features.Has(p.Features, f) }`.
- LoadPolicyRaw: `p.Features, _ = features.Effective(features.LoadConfig(ctx, st, projectID), root != "")`.
- Resolve: bỏ `git.commit/git.branch` khi tắt git, `ops.process/ops.container` + Processes/Containers khi tắt ops, `commands.run` + Commands/Safe khi tắt checks.
- engine: `QuickCheck: policy.QuickCheck && policy.Has(features.Checks)` (2 chỗ); office-tools.md thành template nhận `DevTools`, `Logs` (ops||monitor), `Checks`; khi bật hết, câu chữ giống hệt bản cũ.
- [ ] Test Resolve: agent Operate, policy tắt ops ⇒ không có `ops.process`, Processes rỗng; tắt checks ⇒ Commands rỗng.
- [ ] Test prompt: bật hết ⇒ chứa nguyên câu cũ; tắt develop ⇒ không có `ops_overview`, `git_status`.

### Task 3: Công cụ và đề xuất

**Files:** Modify `internal/officetools/officetools.go` (ToolsFor), `internal/actions/actions.go` (Propose, Decide); tests trong package tương ứng.

- ToolsFor: khi `sc.ProjectID != "" && !sc.Office`, `on := features.For(ctx, t.store, sc.ProjectID)`, bỏ tool `!features.ToolOn(on, x.Name)`; enum `action` của `propose_action` chỉ còn loại `ActionOn`; bỏ hẳn `propose_action` khi enum rỗng.
- Propose: `if !features.ActionOn(features.For(...), kind)` ⇒ `ErrFeatureOff` ("tính năng … đã tắt trong project này").
- Decide (approve): kiểm lại, nếu tắt ⇒ status `failed`, detail lý do, không chạy.
- [ ] Test: ToolsFor không có `git_status` khi git tắt; có `ops_overview` khi chỉ monitor bật; Propose `git_commit` lỗi; thẻ `restart_process` pending, tắt ops, Decide(approve) ⇒ failed.

### Task 4: API

**Files:** Create `internal/api/features.go` (+ test); modify `internal/api/org.go` (repoDTO.Features, routes), `git.go`, `ops.go`, `monitors.go` (bọc route theo project), `incidents.go`.

- `GET /api/features` (registry), `GET/PUT /api/projects/{id}/features` (PUT admin, audit `project.features`, phát sự kiện thay đổi project như các PUT project khác).
- `repoDTO.Features []string \`json:"features"\``.
- `s.feature(f, h)`: route `/api/projects/{id}/…` của git/ops/compose/monitors trả `403 {"error":"feature_off","feature":f,"reason":…}`.
- incidents: bỏ monitor khi monitor tắt, process crashed khi ops tắt. monitor `maybeAnalyse`: skip khi monitor tắt (không tốn AI).
- [ ] Test: PUT rồi GET; `GET /api/projects/{id}/git` 403 khi git tắt; incidents không có monitor khi tắt.

### Task 5: Dashboard – dữ liệu và ẩn

**Files:** `composables/useOffice.ts` (Project.features, `hasFeature`), `layouts/default.vue`, `pages/projects/[id]/index.vue`, `components/OpsPanel.vue`, `components/PolicyPanel.vue`, `ChatPanel.vue` (BranchMenu), `utils/watchWidgets.ts`, `WatchBox.vue`, `WatchPicker.vue`.

- `hasFeature(p, f) = !p?.features || p.features.includes(f)`.
- Sidebar/tab: ẩn `ops` khi !ops && !monitor; tab lạ quay về chat.
- OpsPanel: processes/containers/env theo ops, monitors theo monitor; section mặc định là cái đầu còn lại.
- PolicyPanel: ẩn mục Gói lệnh + Quick check khi !checks (form vẫn giữ giá trị cũ khi lưu).
- GitBar (index.vue) và BranchMenu theo git.
- Widget: `feature?` trên def (process/container/consoles = ops, monitor/incidents = monitor, git = git); WatchBox hiện "Tính năng đã tắt" khi project của box tắt; picker ẩn khi không project nào bật.

### Task 6: Dashboard – thẻ cài đặt

**Files:** Create `components/FeaturePacks.vue`; modify `pages/projects/[id]/index.vue` (tab info), `i18n` vi/en.

- Thẻ gói: switch lớn + danh sách switch tính năng; xám kèm lý do `no_folder`; tắt ops/monitor khi còn thứ đang chạy ⇒ confirm "Còn N … đang chạy — vẫn tắt? (chúng vẫn chạy)".
- Lưu: `PUT /api/projects/{id}/features`, rồi refresh project.
- [ ] `pnpm … typecheck`, `node dashboard/scripts/check-i18n.mjs`, kiểm UI thật trên demo (desktop + `--mobile`).

### Task 7: Tài liệu

- ADR mới trong `docs/DECISIONS.md`, dòng tiến độ `docs/PLAN.md`, `docs/UI.md` (tab Thông tin).
