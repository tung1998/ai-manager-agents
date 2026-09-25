# agent-office: Kế hoạch triển khai

## 1. Tổng quan

`agent-office` mô phỏng một phòng ban gồm các AI agent phân cấp, giám sát và phân tích hệ thống phần mềm của một project.

| Cấp | Vai trò | Được làm | Không được làm |
|---|---|---|---|
| L1 Director | Điều phối, tổng hợp, chốt kết luận, báo cáo người | Giao việc cho manager, mở debate, viết kết luận | Tự gọi tool lấy dữ liệu |
| L2 Manager | Một góc nhìn (Tech Lead, Data Analyst, Risk/Security…) | Phân tích, đặt giả thuyết, giao worker, phản biện | Tự gọi tool lấy dữ liệu |
| L3 Worker | Lấy dữ liệu qua MCP/tool | Trả JSON chuẩn, lưu raw data | Kết luận, side effect khi chưa duyệt |

Framework tổng quát: áp dụng cho project mới chỉ bằng `office.config.json` và role template, không sửa core.

Tài liệu liên quan: [ARCHITECTURE](ARCHITECTURE.md), [DATA_MODEL](DATA_MODEL.md), [INTERFACES](INTERFACES.md), [CLI](CLI.md), [UI](UI.md), [DECISIONS](DECISIONS.md).

## 2. Stack

| Thành phần | Chọn | Ghi chú |
|---|---|---|
| Core + CLI | Go ≥ 1.23, `spf13/cobra` | Một binary `office` |
| Orchestration | Tự build (ADR-001) | State machine trên DB |
| Storage | SQLite `modernc.org/sqlite` / Postgres `pgx/v5` | Migration `pressly/goose` |
| Scheduler/queue | Bảng `schedules` + `runs`, cron `robfig/cron/v3` | Không Redis (ADR-004) |
| Config validate | `santhosh-tekuri/jsonschema/v6` | Draft 2020-12 |
| HTTP | `net/http` Go 1.22 ServeMux, SSE | Không framework |
| Dashboard | Nuxt 4 + @nuxt/ui, pnpm | App riêng (ADR-010) |
| Runtime M0 | `claude-code` (`claude -p`), `api` (Anthropic + OpenAI-compatible) | M5 thêm `codex`, `gemini` |
| Notify M1 | Discord webhook | M2 thêm Telegram, Slack |
| Deploy server | docker-compose: `postgres`, `office`, `dashboard` | |

## 3. Giả định

Các điểm nhỏ được tự quyết. Đổi được mà không ảnh hưởng kiến trúc.

1. Pilot là `storefront-v5`. Nguồn dữ liệu có MCP sẵn: Graylog và Metabase (qua SenPrints Internal MCP Hub), code repo (filesystem/git). Stripe, Redis, tracking chưa có MCP riêng nên ghi "đề xuất kết nối".
2. Claude Code CLI đã cài trên máy chạy. Codex và Gemini CLI chưa cài, nên adapter của chúng để M5.
3. Một `office` instance phục vụ một project. Nhiều project thì chạy nhiều instance hoặc nhiều config (multi-tenant để sau M5).
4. Người dùng dashboard là team nội bộ, M4 dùng token local. Passkey/RBAC để sau.
5. Múi giờ mặc định `Asia/Ho_Chi_Minh`. Ngôn ngữ báo cáo mặc định tiếng Việt, cấu hình được.
6. Chi phí tính bằng USD. Claude CLI trả cost thật. Runtime `api` ước tính từ bảng giá (`cost_estimated=true`).
7. Budget mặc định pilot: 5 USD/ngày toàn phòng, 1 USD/incident. Chỉnh trong config.
8. Worker data lớn (log, query result) lưu dạng blob trong DB, giới hạn 1 MB/output. Vượt thì cắt và lưu file trong `.office/blobs/`.
9. Webhook đầu tiên hỗ trợ: Sentry và Graylog alert (generic HMAC/token). Nguồn khác thêm bằng `TriggerSource` plugin.
10. Không commit `.office/` (DB local, blobs, history) vào git của project được giám sát.

## 4. Tham khảo từ `senprints-agents`

`agent-office` là dự án **độc lập, xây từ đầu**, không extend và không copy code từ `senprints-agents`. Bảng dưới chỉ liệt kê **ý tưởng** đã được kiểm chứng ở đó để cân nhắc khi tự thiết kế. Code viết mới theo kiến trúc của `agent-office`. File nguồn tính từ root của `senprints-agents`.

| Ý tưởng | Tham khảo tại | Áp dụng ở |
|---|---|---|
| `claude -p` stream-json, prompt qua stdin, `--append-system-prompt-file`, `--resume --fork-session` và chạy lại khi mất session | `executor.go:220`, `executor.go:618` | `internal/runtime/claudecode` |
| Cộng dồn nhiều `result` event để không đếm thiếu cost | `executor.go:528` (`mergeResultEnvelopes`) | `claudecode` |
| Chuẩn hóa output về một result envelope | `api_executor.go:784` | `runtime.ResultEnvelope` |
| Capability matrix theo runtime | `runtime_caps.go:23` | `runtime.Capabilities` |
| Probe binary runtime đã cài | `runtime_probe.go` | `office doctor`, `initscan` |
| Bảng giá model + override file | `llm_pricing.go` | `internal/budget` |
| Guard: daily cap → auto-disable kèm mã, runs/giờ, failure kill-switch, chain depth cap | `agent_guard.go` | `internal/budget` |
| Bộ field rule của agent (instructions, tools, strict mode, permission, retry, cost cap, notify, debounce, chain) và `routes.json` | `agent_json.go`, `routes.json` | khối `rules` + `triggers.routes` (ADR-013) |
| Strict MCP config 0600 + verify `system/init`, kill khi lệch | `executor.go:421`, `executor.go:866-946` | `internal/worker` |
| Queue `SKIP LOCKED`, reset job `running` khi restart | `queue.go:115`, `queue.go:188` | `internal/storage/postgres` |
| Debounce webhook bằng `next_attempt_at` | `agent_debounce.go` | `internal/scheduler` |
| Verify webhook HMAC/Ed25519/token | `verify/` | `internal/api` |
| Agent emit JSON, service quyết định alert | `crisp_alert.go` | `internal/orchestrator` (ADR-008) |
| Rollup chi phí theo giờ + failure class | `internal/analytics`, bảng `job_rollup_hourly` | `cost_ledger`, màn Chi phí |

**Không lấy theo:** flat `package main`, bảng `agents` 150+ cột, chạy lại `schema.sql` mỗi boot, scrape `/usage` qua pty, phân loại read-only theo tên tool, logic Crisp/seller-bridge/Jira trong core.

## 5. Milestones

Thứ tự theo prompt, có 3 điều chỉnh:
- **`init` tĩnh lên M0.** Quét manifest + detect runtime không tốn token, và `doctor` cần nó để bootstrap config pilot. Phần LLM phân tích và ước lượng nhân sự vẫn ở M3.
- **Notify M1 chỉ Discord.** Pilot đã dùng Discord hook. Telegram/Slack sang M2 dưới dạng plugin để kiểm chứng `NotifyAdapter`.
- **Interface codex/gemini chốt từ M0.** Adapter vẫn làm ở M5, nhưng capability matrix được thiết kế đủ từ đầu để M5 không sửa core.

### Tiến độ

| Hạng mục | Trạng thái |
|---|---|
| Go module, CLI `office` (`run`, `user`, `config validate`) | Xong |
| `internal/config`: load + validate JSON Schema nhúng | Xong (chưa có kiểm tra ngữ nghĩa) |
| `internal/storage`: interface, SQLite + goose, contract test | Xong cho users, sessions, audit_log |
| `internal/auth` + API đăng nhập, quản lý tài khoản, audit (ADR-014) | Xong |
| Dashboard Nuxt: login, tổng quan, tài khoản, audit, đổi mật khẩu | Xong |
| Docker compose (office + dashboard, SQLite volume) | Xong |
| Kết nối AI: Anthropic, OpenAI, API tương thích, Claude CLI, Codex CLI; key mã hóa; kiểm tra + gửi thử (ADR-016) | Xong |
| Mô hình tổ chức: 3 mẫu có sẵn (solo, team, tam quyền), nhân bản, khôi phục, áp vào repo, sửa agent (ADR-015) | Xong |
| Quản lý repo, chế độ project/máy, `office init` chọn mô hình | Xong |
| Dashboard: Repo, Mô hình mẫu, Kết nối AI, sơ đồ tổ chức + sửa agent, checklist thiết lập | Xong |
| Thiết lập project bằng AI: quét, đọc file agent sẵn có, đề xuất mô hình + tinh chỉnh agent, duyệt rồi áp (ADR-018) | Xong |
| Project không thư mục (helper toàn máy), chọn thư mục bằng cây (ADR-017) | Xong |
| Lịch sử chỉnh sửa + khôi phục, export/import config (thư mục cho git, file cho dashboard), backup, chặn key theo URL mới (ADR-019) | Xong |
| Lịch sử lượt gọi AI, chi phí (thật hoặc ước tính), trần ngân sách theo ngày cho office và project, trang Chi phí (ADR-020) | Xong |
| Cài và đăng nhập Claude Code / Codex ngay trên dashboard (ADR-021) | Xong |
| Chat với agent trong project: stream, công cụ đọc, diff được duyệt rồi mới áp; Claude Code / API / Codex (ADR-022) | Xong |
| Việc cho cả mô hình: Solo, Team (lập kế hoạch → làm song song → tổng hợp), Tam quyền (biểu quyết, phủ quyết, kiểm tra) (ADR-023) | Xong |
| Tự động hóa: quét và quản lý skill, agent, MCP (máy, project, riêng máy), thư viện, kiểm tra an toàn, MCP phổ biến và tìm trong MCP Registry; tab Skills & MCP trong project, trang Thư viện, sidebar project lồng nhau (ADR-024) | Xong |
| Gọi skill bằng "/" và đính kèm ảnh, PDF, file chữ trong Chat và Việc (ADR-025) | Xong |
| Vận hành: quét lệnh của project, chạy/dừng/tự chạy lại như pm2, log trực tiếp, CPU/RAM/cổng, hỏi agent từ log (ADR-026) | Xong |
| Vận hành: container (docker compose): trạng thái, cổng, CPU/RAM/mạng, bật/dừng/chạy lại/gỡ theo stack hoặc service, log trực tiếp, hỏi agent (ADR-026) | Xong |
| Giám sát: theo quy tắc (HTTP, cổng, heartbeat) và có AI (bật/tắt, trần chi phí) | Chưa làm |
| Việc định kỳ, cảnh báo, `doctor` | Chưa làm |

### M0: Nền móng

**Mục tiêu.** Có binary `office` đọc config, lưu DB, gọi được một worker qua `claude-code` hoặc `api`, và `doctor` báo đúng tình trạng môi trường.

**Task.**
1. Khởi tạo Go module, cấu trúc `cmd/office`, `internal/*`, cobra CLI khung.
2. `internal/config`: load + validate schema, kiểm tra ngữ nghĩa mà JSON Schema không diễn đạt được (worker/runtime/mcp server/notify channel được tham chiếu phải tồn tại, id không trùng, `min_minutes ≤ max_minutes`, tổng `rules.daily_cost_limit_usd` ≤ `budget.daily_usd`, `rules.permission_mode` hợp lệ với capability của runtime, `on_success_agent_id` không tạo vòng), resolve `*_env`, ghi file kèm history.
3. `internal/storage`: interface + driver SQLite, goose migration v1 (tất cả bảng trong DATA_MODEL). Driver Postgres chạy cùng test suite.
4. `internal/runtime`: interface, `ResultEnvelope`, `Router` có fallback, adapter `claudecode` và `api` (Anthropic Messages + OpenAI-compatible).
5. `internal/budget` + `internal/audit`: ghi run, cost ledger, guard ngày/agent.
6. `internal/mcp`: registry, probe server, đọc annotation `readOnlyHint`.
7. `internal/worker`: chạy một worker (code-worker trên storefront-v5), validate output, lưu `worker_outputs`.
8. `internal/initscan` bản tĩnh: manifest, SDK, infra, `.mcp.json`, tên biến `.env.example`, runtime đã cài, API key có trong env.
9. Lệnh `office doctor`, `office init --static`, `office worker exec <id> "<query>"` (lệnh debug).

**Definition of Done.**
- `office doctor` trên storefront-v5 báo: runtime khả dụng, MCP kết nối, worker nào read-only, budget còn lại. Exit code khác 0 nếu có lỗi chặn.
- `office worker exec code-worker "liệt kê các trang checkout"` trả JSON đúng schema, có dòng trong `runs`, `worker_outputs`, `cost_ledger`.
- Tắt `claude` CLI (đổi PATH) thì Router fallback sang `api` và ghi `fallback_index=1`.
- Test storage pass trên cả SQLite và Postgres.

**Cách test.**
- Unit: config validate (fixture hợp lệ/không hợp lệ), Router fallback với fake adapter, budget guard, envelope merge.
- Contract test cho `StorageAdapter`: một suite chạy trên cả hai driver (Postgres qua testcontainers hoặc docker-compose).
- Golden test cho parser stream-json của claude-code (fixture ghi lại từ CLI thật).
- Manual: chạy `doctor` và `worker exec` trên storefront-v5.

### M1: Một manager chạy được

**Mục tiêu.** Tech Lead chạy heartbeat tự động trên storefront-v5, có memory, ghi blackboard, gửi Discord khi bất thường.

**Task.**
1. `internal/memory`: 3 lớp, entries, compaction (ADR-006).
2. `internal/blackboard`: threads, findings, validate evidence (ADR-007).
3. `internal/heartbeat`: vòng 6 bước, mode normal/suspicious/incident, tự đặt `next_check_at` trong khoảng min/max của config.
4. `internal/scheduler`: ticker, claim run, lease, reset khi restart, cron.
5. `internal/orchestrator`: dựng prompt từ role template + memory + delta, parse output schema, xử lý escalate.
6. `internal/notify` + Discord adapter.
7. Worker thêm: graylog-worker (MCP Hub).
8. `internal/api` tối thiểu: `/healthz`, webhook ingress Graylog có verify + dedupe.
9. Lệnh `office run`, `office status`.

**Definition of Done.**
- `office run` chạy liên tục 48 giờ trên storefront-v5 không lỗi treo. Restart giữa chừng thì tiếp tục đúng lịch.
- Mỗi heartbeat chỉ lấy delta từ `last_checked_at`, thấy rõ trong `worker_outputs.query`.
- Giả lập lỗi 5xx tăng trong Graylog thì mode chuyển `suspicious`, `next_check_at` rút ngắn, Discord nhận tin có link evidence.
- Memory `long_term` vượt ngưỡng thì compaction chạy, version mới có `compacted_from`.
- Chi phí 24 giờ nằm trong budget, `office status` hiển thị đúng.

**Cách test.**
- Unit: tính `next_check_at` theo mode, compaction trigger, evidence validation.
- Integration với fake runtime (trả output kịch bản) để kiểm vòng heartbeat end-to-end không tốn token.
- Replay test: đưa bộ log Graylog đã ghi lại vào fake worker, kiểm manager phát hiện anomaly.
- Soak test 48 giờ trên pilot.

### M2: Phòng ban đầy đủ

**Mục tiêu.** Director + 3 manager (Tech Lead, Data Analyst, Risk/Security), debate protocol, `office ask`.

**Task.**
1. Director agent: nhận escalate, mở incident, điều phối debate, viết kết luận.
2. `internal/debate`: pha independent (ẩn finding của manager khác), cross_review tối đa N vòng, synthesis. Dừng khi hết round, hết budget, hoặc đồng thuận.
3. Bảng `incidents`, `approval_requests`, lệnh `office approve|reject`.
4. Manager hỏi manager (`ask_manager`) qua blackboard question.
5. `office ask "<câu hỏi>"`: tạo thread on-demand, Director quyết định manager nào tham gia.
6. Trigger cron báo cáo sáng (Director tổng hợp 24 giờ).
7. Notify plugin Telegram + Slack, inbound ask từ Discord.
8. Worker thêm: metabase-worker.
9. Cảnh báo budget theo incident.

**Definition of Done.**
- Một incident mẫu (checkout error tăng + doanh thu giảm) sinh đủ: finding độc lập của 3 manager, ít nhất 1 rebuttal, kết luận Director có evidence, mức tự tin, phương án, rủi ro.
- Log chứng minh ở pha independent không manager nào nhận finding của manager khác trong prompt.
- Debate dừng đúng khi chạm `max_rounds` hoặc `token_budget`.
- `office ask` trả lời trong < 3 phút với câu hỏi đơn giản, có trích evidence.
- Báo cáo sáng gửi đúng giờ.

**Cách test.**
- Scenario test với fake runtime: kịch bản debate đồng thuận, bất đồng, hết budget.
- Prompt-isolation test: assert nội dung prompt pha independent.
- Manual: 3 incident thật hoặc giả lập trên pilot, người đánh giá chất lượng kết luận.

### M3: `init` thông minh

**Mục tiêu.** `office init` trên repo bất kỳ đề xuất cơ cấu hợp lý, người duyệt rồi mới ghi file.

**Task.**
1. Mở rộng `initscan`: migrations/models, cấu trúc module, docker/k8s/terraform.
2. Tóm tắt repo (không đưa toàn bộ code) và gửi LLM để nhóm domain.
3. Quy tắc ước lượng nhân sự: managers 2–5 (luôn Tech Lead, có dữ liệu kinh doanh thì Data Analyst, có payment/PII thì Risk/Security, repo nhỏ thì gộp vai), workers = 1 mỗi nguồn có MCP + 1 code-worker. Mỗi đề xuất có `reason`.
4. Chọn runtime/model theo vai trò + budget, ước tính token/ngày.
5. Màn hình duyệt TUI: Tạo / Chỉnh / Hủy.
6. Chạy lại trên repo đã có config thì chỉ ra diff.
7. Sinh `knowledge.md` (kiến thức project cho các agent).
8. `office import senprints`: chuyển agent export của senprints-agents sang config theo bảng đối chiếu ADR-013.

**Definition of Done.**
- Trên storefront-v5, đề xuất khớp config pilot đang chạy ở mức vai trò.
- Trên `backend-apis` (repo thứ hai), đề xuất có Risk/Security vì có payment, có `reason` cho từng agent.
- Chạy lại không ghi đè, chỉ hiện diff.
- Chi phí LLM cho một lần init < 0.5 USD.

**Cách test.** Fixture repo nhỏ (Node, PHP, Go, Python) cho quét tĩnh. Snapshot test cho đề xuất với fake LLM. Manual trên 2 repo thật.

### M4: Dashboard

**Mục tiêu.** Nuxt dashboard cho org chart, agent detail, blackboard, incidents, chi phí, setup wizard.

**Task.**
1. `internal/api`: REST + SSE theo [UI.md](UI.md), auth token local.
2. `dashboard/`: Nuxt 4 + @nuxt/ui, 6 màn hình.
3. Ghi config qua API (validate schema, history, diff preview).
4. docker-compose cho chế độ server.

**Definition of Done.**
- Mọi thao tác trong UI cập nhật `office.config.json` và CLI thấy ngay.
- Blackboard và trạng thái agent cập nhật realtime qua SSE.
- Duyệt/Từ chối incident trong UI tương đương `office approve|reject`.
- `docker compose up` chạy được stack Postgres.

**Cách test.** API contract test. Playwright E2E cho 6 màn hình (tận dụng kinh nghiệm `storefront_playwright_test`). Manual trên pilot.

### M5: Tự cải thiện

**Mục tiêu.** `tune`, `learn`, runtime `codex` và `gemini`.

**Task.**
1. `office learn`: tính lại baseline từ dữ liệu thật, cập nhật `knowledge.md`, đánh dấu pattern nhiễu (finding bị người từ chối nhiều lần).
2. `office tune`: đọc lịch sử (chi phí, tỷ lệ finding hữu ích, tỷ lệ approve) và đề xuất diff cơ cấu hoặc model.
3. Adapter `codex` (`codex exec --json`) và `gemini` (`gemini -p`, output JSON).
4. Đánh giá chất lượng agent theo tuần (ý tưởng từ skill `daily-agent-quality-review` của repo cũ).

**Definition of Done.**
- `learn` giảm số finding nhiễu ở tuần sau so với tuần trước trên pilot.
- `tune` đề xuất ít nhất một thay đổi có số liệu minh chứng.
- Đổi runtime một manager sang `codex` hoặc `gemini` chỉ bằng config, không sửa core.

**Cách test.** Contract test chung cho mọi `RuntimeAdapter` (cùng bộ fixture). A/B một tuần trên pilot.

## 6. Rủi ro

| # | Rủi ro | Tác động | Giảm thiểu |
|---|---|---|---|
| 1 | **Chi phí token vượt kiểm soát** khi nhiều manager heartbeat dày và debate nhiều vòng | Cao | Budget cứng theo ngày/agent/incident, auto-disable, chỉ xử lý delta, worker dùng model rẻ, heartbeat thích ứng giãn khi bình thường, prompt caching |
| 2 | **Kết luận sai nhưng tự tin** (hallucination, groupthink) | Cao | Evidence bắt buộc bằng code, pha independent, confidence + risks bắt buộc, người duyệt mọi side effect, đo tỷ lệ approve |
| 3 | **Prompt injection từ dữ liệu tool** (log, ticket, tin nhắn chứa lệnh) | Cao | Worker read-only + strict MCP + verify tool surface, bọc `<tool_data>`, agent không có tool gửi tin (ADR-008), audit log |
| 4 | CLI headless thay đổi format output giữa các version | Trung bình | Golden test parser, pin version trong `doctor`, fallback sang `api` |
| 5 | Nhiễu: quá nhiều finding vô ích làm người bỏ qua cảnh báo | Trung bình | Baseline, ngưỡng severity cho notify, `learn` đánh dấu nhiễu, báo cáo gộp |
| 6 | Tổng quát hóa sớm làm chậm pilot | Trung bình | Pilot storefront-v5 trước (ADR-012), `init` thông minh để M3 |
| 7 | Hai dialect storage lệch hành vi | Thấp | Contract test chung, migration cùng version |
| 8 | Dashboard Nuxt riêng làm chế độ local phức tạp | Thấp | `office ui` tự khởi động build có sẵn, CLI vẫn đủ dùng không cần UI |

## 7. Việc đầu tiên ở M0

1. Viết `internal/config` + test validate `examples/office.config.example.json`. Schema là hợp đồng chung giữa CLI, server và UI, nên chốt sớm nhất.
2. Viết adapter `internal/runtime/claudecode` (parser stream-json, result envelope chung), kèm golden fixture ghi từ `claude -p` thật. Tham khảo cách `senprints-agents` xử lý các lỗi thực tế, nhưng code viết mới.
3. Migration v1 SQLite cho `runs`, `worker_outputs`, `cost_ledger`, rồi chạy thử `office worker exec code-worker` trên storefront-v5.
