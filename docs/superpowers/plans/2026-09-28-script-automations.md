# Tự động hóa chạy code (ADR-041) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use checkbox (`- [ ]`).

**Goal:** Automation có hành động `script` chạy code không tốn token AI, gọi agent khi lỗi hoặc khi script yêu cầu, và agent tạo được automation qua đề xuất chờ duyệt.

**Architecture:**
- Mở rộng `internal/trigger` (thêm `script.go`: chạy script, thu output, quyết định có gọi AI không).
- Mở rộng storage (migration dựng lại hai bảng).
- Mở rộng actions và officetools (`propose_automation`), dashboard (trình sửa, job, thẻ đề xuất).

**Spec:** `docs/DECISIONS.md` ADR-041 (dựa trên ADR-040).

## Global Constraints
- Script ≤64KB. Timeout mặc định 300 giây, tối đa 3600. Output giữ 64KB cuối.
- Interpreter: `bash`, `node`, `python3`.
- `create_automation` và `update_automation` không bao giờ tự duyệt, và chỉ admin duyệt được.
- Job loại `script`. Job con có `trigger=escalate` và `parent_job_id`.
- Chữ trên dashboard phải qua i18n. Commit kết thúc bằng dòng Co-Authored-By.

## Review Focus
1. Script sinh process con chạy lâu hơn timeout: cả group phải bị kill, job ra `timeout`.
2. Output hàng MB: chỉ giữ 64KB cuối, không làm tràn bộ nhớ.
3. Script vừa lỗi vừa in `@@agent:`: chỉ tạo đúng một job con.
4. Agent đề xuất cron sai hoặc ngôn ngữ lạ: bị từ chối ngay khi đề xuất, chưa tới bước duyệt.
5. Job con được tạo khi tự động hóa đã tắt, hoặc agent đã bị xóa: job ra `skipped` hoặc `failed` rõ ràng, không treo.

### Task 1: Storage — migration 00021, kiểu dữ liệu, `SetOutput`
- Test (sqlite):
  - tạo job `kind=script`, rồi `SetOutput(id, out, code)`, `Get` phải có `Output`/`ExitCode`;
  - lọc `List(Kind: "script")`;
  - tạo automation `Action=script` với `Script` và `Escalate`, đọc lại phải đúng.
- Migration dựng lại `jobs` (CHECK kind thêm 'script'; cột `output TEXT`, `exit_code INTEGER NULL`, `parent_job_id TEXT`) và `automations` (CHECK action thêm 'script'; cột `script`, `escalate` là JSON).

### Task 2: Chạy script (`internal/trigger/script.go`)
- `RunScript(ctx, dir string, s storage.AutomationScript, env []string, stdin string) (output string, exitCode int, timedOut bool, err error)`.
- Test:
  - bash `echo hi; exit 3` → output `hi`, exit 3;
  - timeout 1s với `sleep 30 & wait` → ra timeout, và chạy xong dưới 3 giây;
  - output 1MB → giữ 64KB cuối;
  - stdin và `OFFICE_PAYLOAD` đọc được.
- `Signals(output) []string`: lấy các dòng `@@agent: …`.

### Task 3: Runner — hành động script và job con
- Test với executor giả:
  - script lỗi, `escalate=failure` → job script `failed/script_error`, có 1 job con `chat_turn` (`parent_job_id` đúng, `trigger=escalate`), prompt chứa output và dòng đánh dấu dữ liệu;
  - `when=signal` và script exit 0 có in `@@agent: x` → 1 job con;
  - `when=never` → không có job con;
  - job con chạy khi tự động hóa đã tắt → `skipped`.
- `execute`:
  - `a.Action == "script"` → `runScript`, `SetOutput`, `Finish`, rồi có thể `escalate()`;
  - job có `ParentJobID` → lấy agent, action, prompt từ `a.Escalate`.

### Task 4: Đề xuất tạo tự động hóa
- Thêm `actions.Kinds`: `create_automation`, `update_automation`. Thêm `ActionArgs.Automation json.RawMessage`.
- `Propose` kiểm tra đặc tả (tên, nguồn, lịch, ngôn ngữ script, kích thước).
- `autoAllowed` luôn false. `Decide` tạo hoặc sửa automation.
- Công cụ `propose_automation` trong officetools.
- Test:
  - đề xuất có cron sai → lỗi;
  - đề xuất hợp lệ → `pending` dù agent ở gói `operate`;
  - duyệt → automation được tạo, bật, có `NextRunAt`;
  - `update_automation` sửa script.

### Task 5: API và DTO
- `automationDTO` và `automationInput` thêm `script`, `escalate`.
- `jobDTO` thêm `output`, `exit_code`, `parent_job_id` (chỉ trả `output` ở `GET /api/jobs/{id}`).
- `ActionDTO` thêm `automation`.
- Kiểm tra đầu vào: `lang` hợp lệ, body tối đa 64KB, timeout trong giới hạn.
- Test API: tạo automation script, chạy ngay, `GET` job có `exit_code`.

### Task 6: Dashboard
- `AutomationEditor`: hành động thứ ba "Chạy code" (ngôn ngữ, ô soạn mono, timeout, "Khi nào gọi AI" cùng agent, hành động, prompt).
- `JobsTable`: loại Code, mở job Code bằng modal hiện output, exit code và link sang job con.
- `ActionCard`: hiện đặc tả tự động hóa.
- Chạy check-i18n, typecheck, build.

### Task 7: Tài liệu, build và chạy thử thật
- Chạy thật: tạo automation script bash `echo ok` và `exit 1`, `escalate=never`, rồi chạy ngay. Kỳ vọng job `failed` có output. Tạo thêm một cái `exit 0`, kỳ vọng `done`. Không gọi AI thật.
