# Registry cấu hình + công cụ chung — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Agent (và sau này trợ lý office, CLI) xem và đề xuất đổi mọi cài đặt qua 4 công cụ chung `describe`, `list`, `get`, `propose_change`; người duyệt trên thẻ có diff; khi duyệt, thay đổi đi qua **đúng handler API** của trang tương ứng (một đường ghi, một nhật ký).

**Spec:** `docs/superpowers/specs/2026-09-28-office-assistant-design.md` §3.2 (phần 2).

## Quyết định (rulings)
- Registry nằm trong `internal/api` (`config_registry.go`): mỗi loại cài đặt khai báo `List`, `Get` (bản đã che bí mật), và **handler + đường dẫn** cho create/update/delete. Apply = gọi handler đó với request nội bộ mang người duyệt (`ctxUser`) và `audit.Who` (agent đề xuất + người duyệt) → validate, ghi, audit y như dashboard.
- Patch: JSON merge lên bản hiện tại, rồi **lọc theo các trường mà input của handler nhận** (reflect tag `json`), vì handler dùng `DisallowUnknownFields`.
- Kiểm tra lúc đề xuất: loại cài đặt có, id có và cùng project (trừ cài đặt cấp office), trường trong patch đều là trường sửa được, op hợp lệ. Lỗi dữ liệu sâu hơn (cron sai…) hiện khi duyệt (thẻ thất bại, có lý do).
- Lúc duyệt: đọc lại bản hiện tại; khác bản đã lưu khi đề xuất → từ chối "đã có thay đổi mới, hãy đề xuất lại".
- Provider: patch không được chứa `api_key` (bị từ chối lúc đề xuất); thẻ có ô dán key, key đi thẳng từ trình duyệt vào lệnh duyệt (`POST /api/actions/:id/approve {api_key}`), AI không thấy.
- Loại v1: `automation`, `agent`, `monitor`, `process`, `policy`, `project`, `usage_settings`, `provider`. (`org_model`: để sau.)
- `actions.project_id` rỗng cho cài đặt cấp office: migration dựng lại bảng actions cho phép NULL.
- Chỉ admin duyệt (như hiện nay mọi thẻ); `propose_change` của agent chỉ đọc cũng tạo thẻ (giống propose_automation: luôn chờ người).

## Tasks
1. Migration actions.project_id nullable; storage/actions: kind `config_change`, `ActionArgs.Change {resource, op, id, patch, before}`; `actions.ConfigApplier` interface + `SetConfig`; Decide gọi applier.
2. Registry + 4 công cụ trong officetools (`SetConfig` với interface `ConfigTools`), wiring ở `api.New`.
3. Thẻ duyệt `config_change` (diff trước/sau, ô key cho provider) trong dashboard.
4. Test: propose→approve cho automation (update), agent (permissions), process (create), provider (key qua thẻ, AI không truyền key), stale → từ chối, id của project khác → từ chối, trường lạ → từ chối; audit ghi agent + người duyệt.
5. ADR-045.
