# Architecture Decision Records

Mỗi ADR gồm: bối cảnh, quyết định, lý do, phương án đã loại. Trạng thái mặc định là **Accepted** trừ khi ghi khác. Khi đổi quyết định, thêm ADR mới với trạng thái `Supersedes ADR-xxx`, không sửa ADR cũ.

---

## ADR-001: Tự build orchestration, không dùng LangGraph / CrewAI / Claude Agent SDK

**Bối cảnh.** Cần hierarchy Director → Manager → Worker, heartbeat định kỳ, debate có giới hạn vòng, memory ngoài, và chạy được trên nhiều runtime (claude-code, codex, gemini, api).

**Quyết định.** Tự build orchestration dạng state machine trên DB. Mỗi bước (heartbeat, debate round, worker request) là một `run` trong queue. LLM được gọi qua `RuntimeAdapter`.

**Lý do.**
- Luồng thực tế đơn giản: heartbeat là 6 bước tuần tự, debate là 3 pha. State machine + bảng DB đủ biểu diễn, dễ debug hơn một graph framework.
- Đa runtime là yêu cầu cốt lõi. CLI headless (`claude -p`, `codex exec`, `gemini -p`) không khớp mô hình "LLM object" của LangGraph/CrewAI.
- Core chọn Go (ADR-002). LangGraph và CrewAI là Python-first.
- State nằm trong DB giúp restart an toàn và dashboard đọc trực tiếp, không cần checkpoint store riêng.
- `senprints-agents` đã chứng minh mô hình "queue + CLI executor" chạy ổn trong production.

**Phương án đã loại.**
| Phương án | Điểm mạnh | Lý do loại |
|---|---|---|
| LangGraph | Graph state, checkpoint, human-in-the-loop sẵn | Python/JS, thêm tầng trừu tượng, không hỗ trợ CLI headless tự nhiên |
| CrewAI | Khái niệm role/crew gần với "phòng ban" | Python, kiểm soát prompt và chi phí kém, khó ép evidence |
| Claude Agent SDK | Subagent, tool, session, MCP sẵn | Khóa vào Anthropic, không phục vụ codex/gemini. Vẫn có thể dùng **bên trong** adapter `claude-code` sau này |

---

## ADR-002: Go cho core + CLI, cấu trúc `cmd/` + `internal/<domain>`

**Bối cảnh.** User chọn Go. Prior art `senprints-agents` cũng là Go nhưng dồn 233 file vào một `package main`, file lớn tới 157K.

**Quyết định.** Go ≥ 1.23. Một binary `office` (CLI + server). Package theo domain trong `internal/`. File giữ nhỏ, mỗi package một trách nhiệm.

**Lý do.** Single binary dễ phân phối cho nhiều project. Có thể port pattern từ repo cũ. Ranh giới package rõ giúp thêm plugin mà không sửa core.

**Phương án đã loại.** TypeScript (hệ sinh thái MCP mạnh nhưng không tái dụng được code Go). Python (phân phối CLI kém gọn).

---

## ADR-003: Storage adapter, SQLite local + Postgres server, migration bằng goose

**Bối cảnh.** Phải chạy local (SQLite) và server (Postgres/Supabase). Repo cũ chạy lại `schema.sql` mỗi lần boot, đã phải thêm nhiều hack (`runOnceMigration`, cột quản lý trong code).

**Quyết định.**
- `StorageAdapter` interface với hai driver: `modernc.org/sqlite` (pure Go, không cần CGO) và `jackc/pgx/v5`.
- Migration versioned bằng `pressly/goose`, thư mục riêng `migrations/sqlite` và `migrations/postgres`, cùng số version.
- Truy vấn viết tay, giữ SQL portable. Chỗ khác dialect (JSON, claim queue) nằm trong driver.

**Lý do.** Pure-Go SQLite giúp cross-compile binary. Goose hỗ trợ cả hai dialect và chạy nhúng được.

**Phương án đã loại.** ORM (GORM/ent): ẩn SQL, khó tối ưu claim queue. Một schema chung cho cả hai: JSONB vs TEXT và `SKIP LOCKED` khác nhau quá nhiều.

---

## ADR-004: Scheduler và queue nằm trong DB, không dùng Redis

**Bối cảnh.** Cần trigger theo `next_check_at` thích ứng, cron, webhook, on-demand. Repo cũ dùng ticker 60 giây cố định và queue Postgres `SKIP LOCKED`.

**Quyết định.**
- Bảng `schedules` giữ `next_check_at` và `cron` (parser `robfig/cron/v3`). Ticker 5 giây poll các dòng đến hạn và enqueue.
- Bảng `runs` là queue kiêm lịch sử. Postgres claim bằng `FOR UPDATE SKIP LOCKED`. SQLite claim bằng `UPDATE … RETURNING` trong transaction `BEGIN IMMEDIATE`.
- Khi khởi động, run đang `running` quá `lease_until` được trả về `pending`.

**Lý do.** Một tiến trình, không thêm hạ tầng. Tải dự kiến vài trăm run/ngày/project, thừa sức.

**Phương án đã loại.** Redis/BullMQ, Temporal, NATS: mạnh nhưng thừa cho quy mô hiện tại. Có thể thêm qua `QueueAdapter` nếu cần scale.

---

## ADR-005: `RuntimeAdapter` là interface thật, trả `ResultEnvelope` chung, fallback ở `Router`

**Bối cảnh.** Repo cũ cố ý dùng `switch` thay interface để log rõ executor nào lỗi. Với 4+ runtime và fallback chain, switch sẽ phình.

**Quyết định.**
- Interface `RuntimeAdapter` gồm `Name`, `Capabilities`, `Probe`, `Run`.
- Mọi adapter trả `ResultEnvelope` cùng shape (mượn `api_executor.go:784` của repo cũ): text, cost, token tách fresh/cache-read/cache-write, turns, model, session_id, `cost_estimated`.
- `runtime.Router` chọn adapter theo `runtime`+`model` của agent, kiểm budget, và thử lần lượt `fallback[]` khi lỗi thuộc nhóm có thể fallback (quota, rate limit, runtime không có, timeout). Lỗi nội dung (output sai schema) không fallback mà retry cùng runtime.
- Mỗi `run` ghi rõ `runtime_used`, `model_used`, `fallback_index`, giữ ưu điểm "biết executor nào lỗi" của repo cũ.

**Phương án đã loại.** Switch theo runtime (khó mở rộng bằng plugin). Chuẩn hóa về OpenAI format (mất thông tin cache của Anthropic).

---

## ADR-006: Memory 3 lớp dạng document, compaction neo vào nguồn thô

**Bối cảnh.** Manager cần context lâu dài bằng bộ nhớ ngoài. Repo cũ cảnh báo rõ: tóm tắt memory từ memory cũ gây drift, nên họ luôn compile lại từ nguồn.

**Quyết định.**
- Mỗi manager có 3 document: `working`, `long_term`, `baseline`. Mỗi document có section cố định và giới hạn ký tự riêng.
- `working` được manager ghi đè mỗi heartbeat (giả thuyết, câu hỏi mở, `next_check_at`, mode).
- `long_term` được append qua **memory entries** có id. Khi vượt ngưỡng, compaction đọc entries + finding/incident gốc được tham chiếu, rồi viết lại. Entry cũ được giữ trong DB (`superseded_by`), không xóa.
- `baseline` là số liệu thống kê do code tính từ `worker_outputs` (median, p95, độ lệch theo khung giờ). LLM chỉ viết phần diễn giải.
- Mỗi lần compaction lưu version mới, cho phép rollback.

**Phương án đã loại.** Vector DB cho memory (thừa ở giai đoạn đầu, khó kiểm tra). Session CLI chạy mãi (mất khi restart, tốn token). Tóm tắt đệ quy (drift).

---

## ADR-007: Evidence bắt buộc, enforce bằng code

**Quyết định.** `blackboard.Post` từ chối finding loại `hypothesis`, `rebuttal`, `conclusion` nếu `evidence_ids` rỗng hoặc trỏ tới `worker_output` không tồn tại. Kết luận của Director phải trỏ tới ít nhất một finding có evidence. Loại `observation` phải trỏ tới worker output. Chỉ `question` được phép không có evidence.

**Lý do.** Chỉ dựa vào prompt thì model vẫn có thể bịa. Ràng buộc ở tầng dữ liệu đảm bảo mọi kết luận truy được về dữ liệu thật.

**Phương án đã loại.** Chỉ nhắc trong prompt. LLM-judge kiểm tra evidence (tốn token, không chắc chắn).

---

## ADR-008: Agent chỉ emit JSON, service quyết định notify và escalate

**Bối cảnh.** Repo cũ dùng pattern này cho Crisp alert: agent trả JSON có cờ, service quyết định gửi Discord.

**Quyết định.** Không agent nào có tool gửi tin. Output của Manager/Director theo JSON Schema (`escalate`, `severity`, `notify`, `ask_manager`). Orchestrator đọc và thực hiện theo policy trong config.

**Lý do.** Chống prompt injection dẫn tới spam hoặc rò rỉ dữ liệu. Policy notify đổi bằng config, không phải sửa prompt.

---

## ADR-009: Worker read-only mặc định, side effect qua approval

**Quyết định.**
- Worker khai báo `mcp_servers` và `tools_allow`. Khi chạy, adapter sinh MCP config tạm (quyền 0600) chỉ chứa server được cấp, bật `--strict-mcp-config`.
- Verify tool surface từ event `system/init` (claude-code). Lệch allowlist thì kill run, ghi `failure_class=tool_surface`.
- Phân loại read-only dựa trên MCP tool annotation `readOnlyHint`, rồi đến allowlist tường minh trong config. Không đoán theo tên tool.
- Hành động có side effect được biểu diễn thành `approval_request`. Chỉ người duyệt qua CLI/UI mới kích hoạt, và việc thực thi do một worker riêng có `side_effects: true` đảm nhận.
- Mọi dữ liệu tool trả về được bọc trong khối `<tool_data>` và role template nhắc rõ đó là dữ liệu, không phải lệnh.

---

## ADR-010: Dashboard là Nuxt app riêng

**Bối cảnh.** User chọn Nuxt app riêng, giống `senprints-agents`.

**Quyết định.** `dashboard/` dùng Nuxt 4 + @nuxt/ui. Gọi REST + SSE của `office` server. Local: `office run --api 127.0.0.1:8787` và `pnpm dev` trong `dashboard/`. Server: docker-compose với `postgres`, `office`, `dashboard`.

**Hệ quả.** Chế độ local cần 2 tiến trình. `office ui` sẽ in hướng dẫn và, nếu có sẵn build, chạy `node .output/server/index.mjs` như tiến trình con.

**Phương án đã loại.** SPA nhúng vào binary bằng `embed.FS` (gọn cho local nhưng user ưu tiên đồng bộ với stack dashboard hiện có).

---

## ADR-011: `office.config.json` là single source of truth

**Quyết định.**
- Một file duy nhất, validate bằng JSON Schema draft 2020-12 (`schema/office.config.schema.json`).
- Secret chỉ tham chiếu tên biến môi trường (`api_key_env`, `webhook_url_env`). Không bao giờ ghi giá trị secret vào file.
- Bảng `agents` trong DB là bản mirror để join, được sync từ file khi `office run` khởi động hoặc khi file đổi. UI ghi vào file qua API, không ghi thẳng vào DB.
- Mọi lần ghi file đều tạo bản sao `.office/config-history/<timestamp>.json` để rollback.

**Phương án đã loại.** Config trong DB (repo cũ): khó review, khó version bằng git. YAML: user yêu cầu JSON.

---

## ADR-012: Pilot trên storefront-v5 trước khi tổng quát hóa

**Bối cảnh.** Prompt yêu cầu làm chạy tốt cho một project thật trước. User chọn `storefront-v5`.

**Quyết định.** M0–M2 tối ưu cho storefront-v5: Nuxt SSR, Stripe, Redis, log qua Graylog (winston-graylog2), Discord hook. Core vẫn giữ interface tổng quát, nhưng role template và worker mẫu viết cho storefront trước. M3 (`init`) được kiểm tra thêm trên một repo khác (đề xuất `backend-apis`) để chứng minh tính tổng quát.

---

## ADR-013: Rule của agent tham khảo cách cấu hình của senprints-agents

**Bối cảnh.** Team đã quen cấu hình agent trong `senprints-agents` (instructions, allowed tools, strict mode, permission, timeout, retry, cost cap, notify, debounce, chain, và `routes.json` map source+event sang handler). Yêu cầu: `agent-office` cấu hình rule theo cách tương tự để team dễ làm quen. `agent-office` vẫn là dự án độc lập, không extend `senprints-agents`, nên bộ field có thể thay đổi theo thiết kế riêng khi cần.

**Quyết định.**
- Mỗi agent (director, manager, worker) có khối `rules` với **tên field giữ nguyên** như định nghĩa agent của senprints-agents (`agent_json.go`).
- Webhook dùng `auth_type`/`auth_name` giống `webhook_auth_type`/`webhook_auth_name`.
- `triggers.routes[]` thay cho `routes.json`: `{source, event, agent, task}`, khớp đầu tiên thắng.
- Lệnh `office import senprints` là tiện ích tùy chọn (M3), không phải ràng buộc tương thích.
- Khác biệt có chủ đích: config nằm trong file thay vì DB (ADR-011). Secret là tên biến env. Trigger của manager là heartbeat, không phải `trigger_type`.

**Bảng đối chiếu.**

| senprints-agents | agent-office | Ghi chú |
|---|---|---|
| `instructions` | `rules.instructions` (+ `instructions_file`) | Nối sau role template |
| `qa_examples` | `rules.qa_examples` | |
| `allowed_tools`, `allowed_skills` | `rules.allowed_tools`, `rules.allowed_skills` | Worker MCP hợp nhất với `source.tools_allow` |
| `strict_mode`, `blocked_builtin_tools` | `rules.strict_mode`, `rules.blocked_builtin_tools` | Mặc định `strict_mode=true` |
| `permission_mode` | `rules.permission_mode` | Mặc định `plan`, validate theo capability runtime |
| `knowledge_search` | `rules.knowledge_search` | Tìm trong `knowledge.md` |
| `timeout_seconds`, `max_attempts`, `retry_delay_seconds` | cùng tên trong `rules` | |
| `daily_cost_limit_usd`, `max_runs_per_hour`, `disable_after_failures` | cùng tên trong `rules` | Thêm budget org + incident ở `budget` |
| `resume_last_session`, `include_last_result` | cùng tên trong `rules` | Manager nên tắt vì đã có memory ngoài |
| `output_format` | `rules.output_format` | Bắt buộc `json` cho mọi cấp của hierarchy |
| `notify_on`, `notify_discord_webhook_url`, `notify_emails` | `rules.notify_on` + `rules.notify_channel` | Kênh khai báo ở `notify.channels`, URL qua env |
| `debounce_seconds`, `debounce_key` | cùng tên trong `rules` và `triggers.routes[]` | |
| `on_success_agent_id`, `chain_condition_path`, `chain_forward_payload` | cùng tên trong `rules` | Chỉ cho director hoặc agent ngoài hierarchy, chain depth ≤ 3 |
| `model`, `effort`, `runtime`, `api_base_url` | `model`, `effort`, `runtime` của agent; `runtimes.*.base_url` | Thêm `fallback[]` |
| `trigger_type=schedule`, `schedule_cron`, `schedule_timezone` | `director.reports[].cron`, `project.timezone` | Manager dùng heartbeat |
| `trigger_type=webhook`, `webhook_auth_type`, `webhook_auth_name` | `triggers.webhooks[]` `auth_type`, `auth_name` | |
| `trigger_type=provider`, `provider_source`, `provider_event`, `routes.json` | `triggers.routes[]` | |
| `api_key`, `webhook_secret` | `*_env` | Không ghi secret vào file |
| `team_ids`, `enabled` | không import | RBAC để sau; import không tự bật agent |

**Phương án đã loại.** Đặt tên field mới theo phong cách riêng: gọn hơn nhưng team phải học lại và không import được.

---

## ADR-014: Đăng nhập dashboard bằng tài khoản email + mật khẩu, làm trước

**Bối cảnh.** Plan cũ để auth ở M4 với token local, passkey sau M5. User yêu cầu làm cơ chế quản lý trước: mở trang admin và đăng nhập bằng tài khoản.

**Quyết định.**
- Tài khoản email + mật khẩu, role `admin` | `member`. Mật khẩu băm argon2id (64 MiB, t=3, p=2), tối thiểu 10 ký tự.
- Phiên là token ngẫu nhiên 32 byte trong cookie `office_session` (HttpOnly, SameSite=Lax, Secure khi HTTPS). DB chỉ lưu SHA-256 của token. Hết hạn tuyệt đối 7 ngày, idle 24 giờ.
- Admin đầu tiên chỉ tạo được bằng CLI `office user create --role admin`. Không có trang đăng ký công khai.
- Khóa đăng nhập 15 phút sau 5 lần sai theo email. Email không tồn tại vẫn chạy verify giả để không lộ qua thời gian phản hồi.
- Chống CSRF: request ghi phải là `application/json`, `Origin` phải thuộc danh sách cho phép hoặc trùng host, cộng SameSite=Lax.
- Dashboard gọi API qua proxy Nitro nên cookie cùng origin với trang. Header `X-Forwarded-*` chỉ được tin khi đến từ `--trusted-proxy`.
- Mọi hành động auth và quản lý user ghi `audit_log`.

**Phương án đã loại.** Passkey như senprints-agents: an toàn hơn nhưng cần HTTPS và RP ID ngay từ đầu, khó cho chế độ local; để sau, thêm song song. Token tĩnh trong file: không phân biệt người dùng, không audit được. OAuth Google: phụ thuộc cấu hình ngoài, cân nhắc khi triển khai server chung.

---

## ADR-015: Repo → mô hình → agent, dữ liệu tổ chức nằm trong DB

**Bối cảnh.** User muốn tạo sẵn vài mô hình tổ chức, chọn khi init, chỉnh chi tiết trong trang admin, và quản lý nhiều repo theo thứ tự repo → mô hình → agent. Office có thể cài trong một project hoặc cài trên máy để quản lý project ở bất kỳ đâu.

**Quyết định.**
- **Mô hình tổ chức** (`org_models`) có hai dạng: *mẫu* trong thư viện (`repo_id` rỗng) và *bản của repo*. Áp mẫu vào repo là **sao chép** toàn bộ mô hình và agent, nên chỉnh repo không ảnh hưởng mẫu, và ngược lại. Mỗi repo có tối đa một mô hình.
- Ba mẫu có sẵn nhúng trong binary (`templates/models/*.json`), seed khi khởi động nếu chưa có, không ghi đè chỉnh sửa. Có "Khôi phục mặc định".
  - **Solo:** một agent lead, như một khung chat.
  - **Team:** trưởng nhóm, PM, kiến trúc sư, project manager, QA lead, kỹ sư, code reader, test runner, monitor. Vai trò và quy trình tham khảo MetaGPT (PM → Architect → PM → Engineer → QA) và ChatDev (CEO/CTO/Programmer/Reviewer/Tester).
  - **Tam quyền phân lập:** ba lead cùng cấp Lập kế hoạch, Thực thi, Giám sát; quyết định 2/3; Giám sát có quyền phủ quyết side effect; N worker báo cáo cho cả ba.
- **Một lõi, một bộ luật:** mọi mô hình được kiểm tra bằng cùng `orgmodel.Validate` (key hợp lệ, có lead, báo cáo cho agent có thật và không phải worker, không vòng, worker phải có cấp trên, solo đúng 1 agent, hội đồng ≥ 2 lead và quorum hợp lệ, veto chỉ gán cho lead). Mọi thay đổi agent được kiểm tra trên toàn mô hình trước khi lưu.
- **Chế độ cài đặt:** `--home` > `OFFICE_HOME` > thư mục `.office` gần nhất đi lên từ cwd (chế độ project) > `~/.agent-office` (chế độ máy). `office init --local` tạo `.office` trong repo và thêm vào `.gitignore`.
- **Nguồn sự thật:** repo, mô hình, agent, kết nối AI nằm trong DB và được sửa qua dashboard hoặc CLI. `office.config.json` (ADR-011) không còn là nguồn sự thật cho phần tổ chức; nó giữ vai trò export/import và cấu hình server. Mô hình export/import dạng JSON `Template`.

**Phương án đã loại.** Mẫu và bản repo dùng chung dữ liệu bằng kế thừa: khó hiểu khi mẫu đổi làm repo đổi theo. Lưu mô hình trong file cạnh repo: không hợp với chế độ máy quản lý nhiều repo và với sửa trên dashboard.

**Supersedes:** một phần ADR-011 (phần tổ chức agent).

---

## ADR-016: Kết nối AI (provider) với key mã hóa và hạng model

**Quyết định.**
- Năm loại kết nối: `anthropic`, `openai`, `openai_compatible` (Ollama, vLLM, OpenRouter…), `claude_cli`, `codex_cli`.
- API key nhập trên dashboard được mã hóa AES-256-GCM. Khóa lấy từ `OFFICE_SECRET_KEY` hoặc file `secret.key` (quyền 0600) trong thư mục dữ liệu. API chỉ trả `has_api_key` và 4 ký tự cuối. Có thể dùng biến môi trường (`api_key_env`) thay vì lưu key.
- Mỗi kết nối map 3 hạng model **strong / balanced / fast**. Agent chọn hạng, hoặc ghi model cụ thể; agent không chọn kết nối thì dùng kết nối mặc định. Đổi nhà cung cấp không phải sửa từng agent.
- "Kiểm tra" liệt kê model (API) hoặc đọc version (CLI), không tốn token. "Gửi thử prompt" gọi model thật. Trạng thái lần kiểm tra cuối được lưu.
- `office init` phát hiện `claude`, `codex` trong PATH và `ANTHROPIC_API_KEY`, `OPENAI_API_KEY` trong env để đề xuất tạo kết nối.

**Phương án đã loại.** Lưu key dạng rõ trong DB. Chỉ cho dùng biến môi trường (bất tiện khi cấu hình từ dashboard). Hard-code tên model GPT (tên đổi thường xuyên; lấy từ API `/models`).

---

## ADR-017: "Repo" đổi thành "Project", đường dẫn là tùy chọn

**Bối cảnh.** User muốn gọi là project, chọn thư mục trực tiếp trên cây, và cho phép bỏ trống đường dẫn để có một helper làm việc trên toàn bộ máy.

**Quyết định.**
- Tên hiển thị, API (`/api/projects`) và CLI (`office project`, giữ bí danh `repo`) dùng "project". Bảng DB và type Go vẫn tên `repos`/`Repo` để tránh migration không cần thiết.
- Project có `scope`: `folder` (gắn một thư mục) hoặc `machine` (không có đường dẫn, là helper toàn máy). Project không đường dẫn bắt buộc có tên. Đường dẫn chỉ unique khi khác rỗng (migration 00003 dựng lại bảng, tắt khóa ngoại trong lúc đó để không xóa mô hình của project).
- Chọn thư mục bằng cây do server cung cấp (`GET /api/fs/dirs`, chỉ admin). Trình duyệt không cho trang web biết đường dẫn tuyệt đối của thư mục local, nhưng server office chạy trên chính máy đó. API chỉ trả tên thư mục con, dấu hiệu project (.git, package.json, go.mod…), và thư mục đã được thêm; không đọc nội dung file. Bỏ qua thư mục ẩn (tùy chọn hiện), `node_modules`, `vendor`; tối đa 500 mục mỗi cấp.

**Phương án đã loại.** `<input webkitdirectory>` của trình duyệt: chỉ cho tên tương đối và buộc tải danh sách file lên, không có đường dẫn tuyệt đối.

---

## ADR-018: Thiết lập project bằng AI (quét + đề xuất + duyệt)

**Bối cảnh.** User muốn mỗi project có bước "init" tự quét project hoặc đọc các file agent sẵn có để viết mô tả và chọn mô hình phù hợp. Cần kết nối AI trước; chưa có thì chuyển sang trang kết nối.

**Quyết định.**
- **Quét tĩnh** (`internal/scan`, không tốn token): manifest và dependency, framework, dịch vụ/SDK (theo dependency và tên biến trong `.env.example`, không bao giờ đọc giá trị), hạ tầng, ngôn ngữ, README (3 KB đầu), và file agent có sẵn: `CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, `.cursorrules`, `.github/copilot-instructions.md`, `.claude/agents/*.md`, `.claude/skills/*/SKILL.md`, `.cursor/rules/*`. Mỗi file cắt 4 KB, tối đa 20 file. Bỏ qua `node_modules`, `vendor`, build output.
- **Đề xuất** (`internal/setup`): gửi bản tóm tắt + thư viện mẫu cho kết nối mặc định ở hạng *strong*. Dữ liệu repo bọc trong `<project_data>` và được coi là dữ liệu, không phải lệnh. AI trả JSON: mô tả, `template_key`, lý do, độ tự tin, tối đa 8 `agent_changes` (update = thêm bối cảnh project vào hướng dẫn; add = agent mới, có thể chuyển từ file agent/skill có sẵn; remove), ghi chú.
- **Người duyệt quyết định:** từng thay đổi có checkbox; đổi mẫu hoặc bỏ chọn sẽ dựng lại và kiểm tra bằng `orgmodel.Validate`. Chỉ áp dụng khi hợp lệ. Agent thêm mới có quyền ghi luôn bật "cần người duyệt".
- **Chưa có kết nối AI hoạt động** → chuyển sang `/providers?next=…`; kiểm tra kết nối thành công thì tự quay lại. API trả `412 {code: no_provider}`.
- Project toàn máy (không thư mục) bỏ qua bước quét và bắt buộc mô tả mục tiêu.
- Chi phí tham khảo: một lần đề xuất cho storefront-v5 bằng claude-opus-5-5 khoảng 0.4 USD, ~45 giây.

**Phương án đã loại.** Cho AI đọc toàn bộ code (tốn token, chậm, rủi ro lộ dữ liệu). Áp đề xuất tự động không qua duyệt. Chỉ dùng luật tĩnh để chọn mẫu (không tận dụng được CLAUDE.md/AGENTS.md và mục tiêu người dùng).

---

## ADR-019: Lịch sử chỉnh sửa, export/import config, backup

**Bối cảnh.** Config nằm trong DB (ADR-015). Tham khảo cách `senprints-agents` bù cho việc này (bảng `agent_revisions`, export agent ra file và tự commit git, script backup), và sửa một lỗ hổng họ đã xử lý.

**Quyết định.**
- **Key không đi theo địa chỉ mới:** đổi Base URL của kết nối đang có key đã lưu thì bắt buộc nhập lại key (`ErrKeyRequiredForNewURL`). Không thì người sửa được URL có thể trỏ sang server của họ rồi bấm Kiểm tra để lấy key.
- **Lịch sử:** bảng `org_revisions` lưu snapshot (dạng `Template`) *trước* mỗi thay đổi của mô hình hoặc agent, kèm người thực hiện; giữ 50 bản mỗi mô hình. Khôi phục thay nội dung tại chỗ (giữ id, giữ liên kết project) và cũng tạo snapshot, nên hoàn tác được. Đổi mô hình của project và khôi phục mẫu có sẵn cũng thay tại chỗ thay vì xóa rồi tạo, để không mất lịch sử.
- **Kết nối AI riêng của agent** được giữ trong `Template` (`provider_id`), nên nhân bản, snapshot, khôi phục không làm mất. Kết nối đã bị xóa thì agent quay về kết nối mặc định.
- **Export/import** (`internal/transfer`): kết nối AI (không key, chỉ `api_key_env` và cờ "đã từng có key"), mô hình mẫu, project và mô hình. Agent trỏ tới kết nối bằng tên để mang sang máy khác. Không export tài khoản, phiên, key. Import khớp theo tên kết nối, key mẫu, đường dẫn project (hoặc tên với helper toàn máy); luôn có bước xem trước; mô hình bị ghi đè được lưu vào lịch sử. Kết nối cần key mà không có `api_key_env` thì bỏ qua kèm hướng dẫn.
- **Dạng thư mục cho git:** `office export <dir> [--commit]` ghi `office.json`, `providers.json`, `templates/<key>.json`, `projects/<slug>.json`; thứ tự ổn định, không timestamp, xóa file thừa. Dashboard dùng một file bundle.
- **Backup:** `office backup` và nút trên dashboard dùng `VACUUM INTO` (an toàn khi server đang chạy) + chép `secret.key`, vào `<dữ liệu>/backups/<thời-gian>/`. Bản backup chứa khóa giải mã nên không đưa lên git.

**Phương án đã loại.** Tự push git từ server (rủi ro đẩy nhầm, cần credential); để người dùng tự `--commit` rồi push. Export kèm key mã hóa (vô dụng ở máy khác vì khác `secret.key`, và dễ bị đưa lên git).

---

## ADR-020: Ghi nhận mọi lượt gọi AI, chi phí và trần ngân sách theo ngày

**Quyết định.**
- Mọi lượt gọi model đi qua một điểm duy nhất `provider.Service.Call`: kiểm tra ngân sách → gọi model → ghi bảng `runs` (loại, project, agent, kết nối, model, token vào/ra, chi phí, nguồn chi phí, thời gian, trạng thái, người thực hiện). Chat và agent sau này dùng cùng điểm này.
- **Nguồn chi phí:** `provider` khi runtime tự báo (Claude Code trả `total_cost_usd`); `estimate` khi tính theo bảng giá (giá Anthropic lấy từ tài liệu claude-api, bản cache 2026-06-24; admin sửa hoặc thêm giá model khác như GPT); `unknown` khi model chưa có giá. Ước tính không tách token cache nên là cận trên.
- **Trần theo ngày** (theo múi giờ máy chạy office): toàn office và từng project. Chạm trần thì lượt gọi bị chặn trước khi tới model, vẫn được ghi với trạng thái `blocked`, API trả `429 {code: "budget"}`. Mức cảnh báo mặc định 80%.
- **Trang Chi phí:** hôm nay so với trần (màu trạng thái luôn kèm icon và chữ), tổng kỳ, số lượt, số lượt chưa rõ giá; biểu đồ theo ngày một màu (có tooltip và chế độ bảng); phân tích theo project và theo model; danh sách lượt gọi gần đây; hộp thoại chỉnh ngân sách và giá.

**Phương án đã loại.** Chặn theo tháng (khó kiểm soát khi một ngày chạy lỗi vòng lặp). Gọi Admin API của Anthropic để lấy chi phí thật (chỉ áp dụng cho API key của tổ chức, không dùng được cho Claude Code hay nhà cung cấp khác).

---

## ADR-021: Cài và đăng nhập Claude Code / Codex từ dashboard

**Bối cảnh.** Người dùng muốn thêm kết nối "Claude Code trên máy" hay "Codex trên máy" ngay cả khi máy chưa cài, và đăng nhập luôn mà không mở terminal.

**Quyết định.**
- Server office chạy trên chính máy đó nên chạy được lệnh cài và lệnh đăng nhập (`internal/clitools`). Mỗi tác vụ chạy trong pseudo-terminal (creack/pty), output (đã bỏ mã ANSI) được dashboard lấy định kỳ. Link đăng nhập và mã thiết bị (dạng `ABCD-1234`) được tách ra thành nút; có ô gửi mã khi CLI hỏi.
- **Chỉ các lệnh định nghĩa sẵn** chạy được, API không nhận chuỗi lệnh:
  - Claude Code: trình cài chính thức `curl -fsSL https://claude.ai/install.sh | bash` (khuyên dùng), `brew install --cask claude-code`, `npm install -g @anthropic-ai/claude-code` (Node 22+). Đăng nhập `claude auth login`; trạng thái `claude auth status --json` (kiểm chứng trên bản 2.1.281).
  - Codex: `brew install --cask codex`, `npm install -g @openai/codex`. Đăng nhập `codex login --device-auth` (URL + mã một lần); trạng thái `codex login status`, chỉ dùng `~/.codex/auth.json` làm dự phòng khi CLI không trả lời.
- Hiện rõ lệnh trước khi chạy; cách cài thiếu công cụ cần thiết (curl/brew/npm) bị vô hiệu. Một tác vụ mỗi công cụ tại một thời điểm, giới hạn 15 phút, hủy được. Chỉ admin; mọi tác vụ ghi audit log.
- Tìm lệnh theo PATH mở rộng (`~/.local/bin`, `~/.claude/local`, thư mục global của npm, Homebrew). Kết nối CLI không ghi đường dẫn cũng dùng cách tìm này, nên công cụ vừa cài dùng được ngay không cần khởi động lại office. `OFFICE_CLI_PATH` cố định nơi tìm (test, máy đặc biệt).
- Cờ `office run --cli-setup` (mặc định bật); docker-compose tắt vì cài vào container không có ý nghĩa.
- Nút "Thêm và kiểm tra" chỉ bật khi công cụ đã cài và đã đăng nhập.

**Phương án đã loại.** Cho nhập lệnh tùy ý (rủi ro thực thi lệnh từ web). Mở terminal web đầy đủ (quá rộng quyền). Chỉ hướng dẫn bằng chữ (không đáp ứng "cài và login luôn").

**Chưa kiểm chứng với tài khoản thật:** luồng `claude auth login` và `codex login --device-auth` được test bằng CLI giả để không đăng xuất hoặc thay tài khoản đang dùng trên máy phát triển.

---

## ADR-022: Chat với agent trong project; agent chỉ đọc, sửa code qua diff được duyệt

**Quyết định.**
- Mỗi project có các cuộc trò chuyện (`conversations`, `messages`), mỗi cuộc với một agent lead hoặc manager của mô hình (mặc định lead đầu tiên). System prompt gồm vai trò, hướng dẫn của agent, tên/thư mục/mô tả project và quy tắc.
- **Agent không bao giờ tự ghi file.** Chỉ có công cụ đọc. Khi cần sửa code, agent trả unified diff trong khối ` + "```diff" + `; office tách thành `patches` (kiểm tra đường dẫn an toàn, không `..`, không `.git`/`.office`, và `git apply --check`). Người có quyền admin bấm Duyệt thì office chạy `git apply`; Từ chối thì ghi lại. Agent chỉ-đọc (read_only) không đề xuất diff. Project toàn máy không áp diff.
- **Claude Code**: chạy headless, cô lập khỏi cấu hình cá nhân (`--setting-sources project,local`, `--strict-mcp-config`, `--disable-slash-commands`), chỉ có `Read`, `Glob`, `Grep`, chặn đọc `.env`/khóa; stream từng đoạn (`--include-partial-messages`); tiếp tục phiên bằng `--resume`, tự chạy lại kèm lịch sử nếu phiên mất. Cô lập giảm chi phí một lượt nhỏ từ ~$0.073 xuống ~$0.023 và không chạy hook/plugin của người dùng. *(ADR-036: nay mặc định dùng cấu hình như CLI của người dùng, cô lập thành tùy chọn theo project.)*
- **Claude API / OpenAI / API tương thích**: office chạy vòng lặp tool (tối đa 20 vòng) với `list_dir`, `read_file`, `search_text`, giới hạn trong thư mục project (chặn symlink ra ngoài, file bí mật, thư mục build). Nội dung trả về của assistant được gửi lại nguyên vẹn (giữ thinking block).
- **Codex**: `codex exec --json --sandbox read-only`, lịch sử gửi dạng transcript.
- Mỗi lượt kiểm tra ngân sách trước, ghi `runs` loại `chat` sau (ADR-020). Một lượt mỗi cuộc trò chuyện tại một thời điểm, tối đa 20 phút, dừng được.
- Dashboard nhận sự kiện qua SSE (`/api/chat/turns/:id/stream`, phát lại từ `Last-Event-ID`), hiển thị chữ đang stream, công cụ đã dùng, và thẻ diff có nút Duyệt/Từ chối. Markdown render bằng `marked` và làm sạch bằng DOMPurify.

**Phương án đã loại.** Cho agent ghi file trực tiếp (không kiểm soát được). Dùng `--permission-prompt-tool` của Claude Code để hỏi duyệt từng lần ghi (chỉ dùng được cho Claude Code, không thống nhất với runtime khác). Chạy Claude Code với cấu hình cá nhân (tốn gấp ~3 lần và chạy hook/plugin không liên quan).

---

## ADR-023: Việc (task) chạy theo cách ra quyết định của mô hình

**Bối cảnh.** Chat mới là một agent. Mô hình Team và Tam quyền cần các agent phối hợp thật.

**Quyết định.**
- Một **Việc** là mục tiêu giao cho cả mô hình của project (`tasks`, `task_steps`). Mỗi bước là một lượt của một agent qua `chat.Engine.Invoke` (cùng công cụ chỉ đọc, system prompt, ghi chi phí loại `task`). Tối đa một Việc chạy mỗi project, 45 phút.
- **single (Solo):** lead làm trực tiếp.
- **hierarchy (Team):** lead lập kế hoạch dạng JSON (`analysis`, `assignments` tối đa 6, hoặc `answer` nếu tự trả lời được) → các agent không phải lead làm song song (tối đa 3 cùng lúc) → lead tổng hợp câu trả lời cuối. Việc giao cho key không tồn tại bị bỏ qua và ghi lại.
- **council (Tam quyền):** người lập kế hoạch (key `planner`, hoặc lead đầu) đề xuất → các lead còn lại bỏ phiếu JSON; người lập kế hoạch tính là đồng ý; thông qua khi đủ `quorum` và không lead có quyền phủ quyết nào phản đối. Không qua thì sửa kế hoạch một lần kèm lý do; vẫn không qua thì Việc kết thúc `rejected`. Qua thì worker làm, Giám sát (key `auditor` hoặc người có quyền phủ quyết) kiểm tra và trả `verdict`; `block_changes` của người có quyền phủ quyết → mọi diff đang chờ bị từ chối với lý do. Thực thi tổng hợp.
- Diff chỉ tách từ bước `work` của agent được phép đề xuất; vẫn cần admin duyệt (bảng `patches` nay nhận cả `task_id`/`step_id`).
- **Chi phí:** mỗi Việc có trần riêng (`budget_usd`, 0 = không trần) cộng trần ngày của office/project; chạm trần thì bước tiếp theo không chạy và Việc `failed` với lý do.
- Dashboard: tab **Việc** trong project, dòng thời gian từng bước (kế hoạch và danh sách giao việc, phiếu bầu, đánh giá, đầu ra, công cụ, chi phí), kết quả cuối, thẻ diff; cập nhật qua SSE `/api/tasks/:id/stream`.

**Giới hạn hiện tại.** Manager chưa giao tiếp xuống worker (một cấp giao việc từ lead). Chưa có lượt hỏi lại người dùng giữa chừng. Kiểm chứng thật với Claude Code cho Team; Tam quyền kiểm chứng bằng model giả.

**Phương án đã loại.** Cho agent tự gọi nhau bằng tool (khó kiểm soát chi phí và vòng lặp). Chạy tất cả agent mỗi lần (tốn và nhiễu).


---

## ADR-024: Tự động hóa, quản lý skill, agent và MCP server trên máy

**Bối cảnh.** Team dùng Claude Code với nhiều skill (`.claude/skills/<tên>/SKILL.md`), subagent (`.claude/agents/*.md`) và MCP server rải rác ở máy và từng project. Cần một chỗ để xem đã cài gì ở đâu, cài lại cho project khác, và cài nhanh các MCP quan trọng.

**Quyết định.**
- **Quét** (chỉ đọc) những nguồn sau:
  - `~/.claude/{skills,agents}` và skill/agent của plugin (chỉ xem).
  - `~/.claude.json`: `mcpServers` của toàn máy, `projects[path].mcpServers` là loại riêng máy.
  - Trong mỗi project Claude Code đã mở và mỗi project office: `.claude/{skills,agents}`, `.mcp.json`, CLAUDE.md, AGENTS.md.
  - Cursor, Claude Desktop, Codex (chỉ xem).

  Giá trị env/header và tham số trông như bí mật được che trước khi trả về dashboard.
- **Nơi cài.**
  - Toàn máy: `~/.claude/...`, hoặc `claude mcp add-json -s user`.
  - Project: `<project>/.claude/...`, hoặc `.mcp.json` để chia sẻ qua git.
  - Riêng máy cho một project: `claude mcp add-json -s local`, chạy trong thư mục project.

  MCP của máy và riêng máy đi qua CLI chính thức để không tự sửa `~/.claude.json`. `.mcp.json` được sửa trực tiếp, bản cũ sao lưu vào thùng rác.
- **Gỡ.** Skill và agent được chuyển vào `<office>/trash` để khôi phục được. MCP gỡ qua `claude mcp remove` hoặc sửa `.mcp.json`. Mọi thao tác nhận tham chiếu `{kind, name, type, path, project_path}` rồi đối chiếu với một lần quét mới, nên client không thể trỏ tới đường dẫn tùy ý. Mục của plugin và công cụ khác không gỡ được.
- **Thư viện** của office nằm trong `<office>/library/{skills,agents,mcp}`, dạng file thường để dễ git, copy hay export, không dùng database. Khi lưu MCP vào thư viện, giá trị env/header được đổi thành ô `{{KEY}}` để thư viện không chứa token.
- **Kiểm tra an toàn** skill và agent trước khi cài hoặc lưu:
  - Từ chối: `curl | sh`, giải mã base64 rồi chạy, `rm -rf /` hoặc `~`, gửi khóa SSH hay biến môi trường ra ngoài.
  - Cảnh báo, cần xác nhận: công cụ Bash, sudo, `git push --force`, gọi mạng, câu "bỏ qua hướng dẫn trước".
- **MCP có sẵn.**
  - Danh mục chọn lọc: Context7, Playwright, Chrome DevTools, GitHub, Sentry, Atlassian, Linear, Notion, Supabase, Filesystem, Fetch.
  - Tìm trong MCP Registry chính thức (`registry.modelcontextprotocol.io/v0/servers`). Remote được ưu tiên vì không cần runtime; không có remote thì dùng gói npm (`npx`), pypi (`uvx`) hoặc oci (`docker`). Env và header cần điền trở thành ô nhập, ô bí mật nhập dạng password.
- **Nhật ký.** Audit ghi hành động, tên và nơi cài, không bao giờ ghi giá trị người dùng nhập.
- **Giao diện theo project.** Skill và MCP được xem và cài trong tab **Skills & MCP** của từng project. Trang **Thư viện** (ở cuối menu, cạnh Mô hình) là kho mẫu và nơi quản lý cài đặt toàn máy. Việc của mọi project hiện trên Tổng quan, không có trang Jobs riêng. Chỉ admin dùng được phần này.
- **Không có trang Agents riêng.** Subagent của Claude Code chỉ là nguồn để thiết lập mô hình: bước quét của thiết lập bằng AI (ADR-018) đã đọc `.claude/agents/*.md` và chuyển thành agent của mô hình. Một trang riêng sẽ gây nhầm với agent của mô hình. API vẫn hỗ trợ `kind=agent` để dùng sau.

**Phương án đã loại.**
- Lưu thư viện trong SQLite: khó xem, sửa và đưa vào git.
- Tự ghi `~/.claude.json`: dễ đụng ghi đồng thời với Claude Code đang chạy.
- Symlink skill từ thư viện vào project: hỏng khi project được clone sang máy khác.

---

## ADR-025: Gọi skill bằng "/" và đính kèm file trong Chat và Việc

**Bối cảnh.** Một đoạn prompt không đủ để giao việc: cần ảnh chụp lỗi, log, tài liệu. Người dùng cũng muốn gọi skill bằng `/tên` như trong Claude Code.

**Quyết định.**
- **Skill.**
  - Ô nhập của Chat và Việc: gõ `/` sẽ hiện skill dùng được trong project (`GET /api/projects/:id/skills`). Thứ tự ưu tiên: skill của project, rồi của máy, rồi của plugin (tên dạng `plugin:skill`).
  - Khi gửi `/tên yêu cầu`, server thay bằng prompt chứa nội dung SKILL.md và danh sách file khác của skill. Cách này chạy được trên mọi runtime vì không phụ thuộc cơ chế skill của Claude Code, vốn vẫn tắt vì chạy ở chế độ cô lập.
  - Tin nhắn và Việc lưu đúng chữ người dùng gõ. Skill không tồn tại thì báo lỗi 400.
- **Đính kèm.**
  - Hỗ trợ ảnh (png, jpg, gif, webp), PDF và file chữ, tối đa 10 MB mỗi file (file chữ 200 KB) và 10 file mỗi lần gửi.
  - Upload bằng JSON base64 để giữ quy tắc CSRF chỉ nhận JSON. File lưu ở `<office>/attachments/<id>/`, gắn với một project; gửi kèm file của project khác sẽ bị từ chối.
  - Dòng tin nhắn và Việc chỉ lưu tham chiếu, trong cột `attachments` (migration 00008).
- **Mỗi runtime nhận file theo cách riêng.** File chữ luôn được chèn thẳng vào prompt. Ảnh và PDF thì:
  - Claude Code: nhận đường dẫn và đọc bằng Read, thư mục được thêm qua `--add-dir`.
  - API Anthropic: block `image` hoặc `document`.
  - API OpenAI: part `image_url` hoặc `file`.
  - Codex: ảnh qua `--image`. PDF chưa đọc được nên chỉ báo cho agent biết là có file.
- **Việc.** Mọi bước của Việc (lập kế hoạch, biểu quyết, làm, kiểm tra, tổng hợp) đều nhận file đính kèm.
- **Xem lại file.** `GET /api/attachments/:id` phục vụ file để xem trước, với `nosniff` và CSP `sandbox`. Chỉ ảnh và PDF giữ đúng loại; mọi file chữ, kể cả HTML/SVG, trả về dạng text/plain.

**Giới hạn.**
- File chỉ được gửi ở lượt có đính kèm. Các lượt sau:
  - API runtime chỉ nhận lại phần chữ của lịch sử.
  - Claude Code vẫn giữ file nhờ session.
- Chưa dọn file đính kèm cũ.

**Phương án đã loại.**
- Upload dạng multipart: phải nới quy tắc CSRF chỉ nhận JSON.
- Bật skill gốc của Claude Code: phá chế độ cô lập và không dùng được cho runtime khác.

---

## ADR-026: Vận hành, chạy và theo dõi lệnh của project (giống pm2)

**Bối cảnh.** Project như storefront-v5 cần chạy `dev`, `build`, `test` và xem log. Về sau cần thêm container và giám sát kiểu Vantage.

**Quyết định.**
- **Quét không dùng AI.**
  - Nguồn đọc: script trong `package.json` (kể cả `apps/*`, `packages/*` của monorepo), `Makefile`, `Procfile`, `go.mod`. Công cụ chạy script (pnpm/yarn/bun/npm) chọn theo lockfile.
  - Script `dev|start|serve|preview|watch` là **service** (chạy liên tục), còn lại là **job** (chạy xong thì dừng). Bỏ qua các hook `pre*`, `post*`, `prepare`.
  - `docker-compose.yml` chỉ được ghi nhận, sẽ quản lý ở bước sau.
- **Định nghĩa** lưu trong bảng `processes` (migration 00009). Chỉ admin tạo, sửa, chạy; lệnh chạy qua `sh -c` trong thư mục nằm trong project.
- **Chạy.**
  - Mỗi tiến trình có process group riêng; dừng = SIGTERM cả nhóm, sau 8 giây thì SIGKILL. PATH giống lúc office cài CLI.
  - Log gồm 3000 dòng gần nhất trong bộ nhớ, bỏ mã màu ANSI, và ghi vào file `<office>/logs/<id>.log` (xoay vòng ở 5 MB). Dashboard xem trực tiếp qua SSE.
  - Cổng được đọc từ log (`localhost:3000`, `listening … port 8080`). CPU và RAM của cả nhóm lấy bằng một lệnh `ps` mỗi 3 giây.
  - Tùy chọn **tự chạy lại** khi service lỗi: chờ 1, 2, 4… tối đa 32 giây; về lại 1 giây nếu lần chạy trước sống quá 1 phút. Tùy chọn **bật cùng office**.
  - Tiến trình dừng khi office tắt. Office không chạy nền thay pm2.
- **Hỏi agent.** Lấy 300 dòng log cuối lưu thành file đính kèm (ADR-025), rồi mở Chat với câu hỏi điền sẵn.
- **Giám sát về sau có hai loại.**
  - Theo quy tắc: HTTP, cổng, heartbeat, tiến trình còn sống. Không tốn token.
  - Có AI: bật/tắt riêng từng mục, có trần chi phí, chỉ gọi khi loại theo quy tắc báo bất thường.

- **Container (docker compose).**
  - Office điều khiển chính file compose của project bằng docker CLI. Tên compose project giữ mặc định (tên thư mục) nên office thấy đúng các container giống như khi bạn chạy `docker compose` bằng tay.
  - Danh sách service lấy bằng `config --services --profile '*'`, gồm cả service nằm trong profile. Trạng thái và cổng lấy từ `ps --all --format json` (đọc được cả dạng mảng lẫn từng dòng). CPU, RAM, mạng lấy từ `docker stats --no-stream`.
  - Thao tác gồm `up -d`, `stop`, `restart`, `down`, `pull`, `build` cho cả stack hoặc từng service (với một service, `down` là `rm -s -f`). Dừng/gỡ cả stack áp dụng cho mọi profile; `up` cả stack giữ mặc định là không bật service trong profile. Gọi `up` với tên service cụ thể thì compose tự bật profile của nó.
  - Thao tác chạy nền, mỗi project chỉ một thao tác tại một thời điểm, output xem trực tiếp như tiến trình. Log container theo dõi bằng `logs --follow --tail 300`.
  - File compose phải là file tìm thấy trong project, tên service phải có trong file đó. Lỗi docker trả về dòng thông báo đầu tiên. Thiếu `docker compose` v2 thì báo rõ.
  - Nút "Hỏi agent" lấy 300 dòng log của container.

**Phương án đã loại.**
- Dùng pm2: thêm phụ thuộc Node toàn cục, khó lấy log và trạng thái về dashboard.
- Gọi Docker Engine API qua socket: phải thêm client và xử lý compose labels thủ công, trong khi CLI đã làm đúng cách compose làm.
- Tiến trình sống tiếp sau khi office tắt: phải tự dò lại PID khi mở office, dễ sót tiến trình mồ côi.

---

## ADR-027: Giám sát theo quy tắc, AI chỉ phân tích khi có sự cố

**Bối cảnh.** Cần theo dõi project giống một uptime monitor (tham khảo Vantage), nhưng không muốn gọi AI liên tục.

**Quyết định.**
- **Loại giám sát** (bảng `monitors`, migration 00010):
  - `http`: GET, mã trạng thái hợp lệ (mặc định 200-399), tùy chọn phải chứa một đoạn chữ, tối đa 5 redirect.
  - `tcp`: kiểm tra mở được cổng.
  - `heartbeat`: service gọi `GET|POST /api/heartbeat/:token` (công khai, token là bí mật). Quá 1,5 lần chu kỳ không có tín hiệu thì Down.
  - `process`: tiến trình của office (ADR-026) còn chạy.
  - `container`: service compose còn chạy và không unhealthy.
- **Lịch chạy.** Kiểm tra mỗi 5 giây xem mục nào đến hạn (chu kỳ từ 10 giây đến 1 ngày), chạy song song tối đa 8 mục. Kết quả lưu vào `monitor_checks`, giữ 7 ngày.
- **Trạng thái.**
  - Down sau 2 lần lỗi liên tiếp để chống báo nhầm; heartbeat và tiến trình thì Down ngay.
  - Mỗi lần đổi trạng thái tạo một sự kiện `up`/`down` trong `monitor_events`. Lần kiểm tra đầu tiên của giám sát mới chỉ tạo sự kiện nếu kết quả là Down.
- **AI (bật riêng từng giám sát, mặc định tắt).**
  - Chỉ khi chuyển sang Down: agent lead của project nhận kết quả kiểm tra gần đây và log (tiến trình/container), rồi phân tích nguyên nhân, việc cần kiểm tra tiếp và cách sửa.
  - Giới hạn: trần USD mỗi 24 giờ cho từng giám sát (mặc định $0.5) và chờ 30 phút giữa hai lần phân tích. Không phân tích khi tiến trình được dừng chủ động.
  - Chi phí ghi vào `runs` loại `monitor` và vào sự kiện.
- **Giao diện.**
  - Mục **Giám sát** trong tab Vận hành: thẻ tổng hợp (Up / Down / Chờ / uptime TB 24h); bảng giám sát với uptime 24h, độ trễ TB, 30 thanh xu hướng, công tắc AI; cột sự kiện có phần phân tích AI.
  - **Gợi ý giám sát** tạo sẵn từ tiến trình, cổng đọc được từ log, service compose và cổng đã publish.
  - Trang Tổng quan có tổng hợp và danh sách các mục đang Down.

**Chưa làm.** Gửi thông báo (Discord/Telegram), trang trạng thái công khai, giám sát từ máy khác.

**Phương án đã loại.**
- Để AI định kỳ tự xem hệ thống: tốn token và kết quả không ổn định.
- Báo Down ngay từ lần lỗi đầu: dễ báo nhầm vì mạng chập chờn.

---

## ADR-028: Nhà cung cấp API bên thứ 3 và thống kê trên trang Kết nối AI

**Bối cảnh.** Tham khảo 9router (bộ định tuyến AI gom nhiều nhà cung cấp, có dashboard token và chi phí): người dùng muốn kết nối nhanh các nhà cung cấp khác, và xem thống kê ngay trên trang Kết nối AI.

**Quyết định.**
- **Danh sách preset** trong `internal/provider/presets.go`, gồm 18 nhà cung cấp nói giao thức chat-completions của OpenAI, chia 4 nhóm:
  - Cổng trung gian: OpenRouter, Together, Fireworks, NVIDIA NIM.
  - Quốc tế: Gemini, DeepSeek, Groq, xAI, Mistral, Cerebras, Perplexity.
  - Trung Quốc: Z.ai (GLM), Moonshot (Kimi), MiniMax, Qwen, SiliconFlow.
  - Chạy trên máy: Ollama, LM Studio.
- **Mỗi preset chỉ giữ:**
  - địa chỉ API;
  - link lấy key và tên biến môi trường quen dùng;
  - có cần key hay không;
  - một dòng ghi chú.

  Preset **không ghi tên model**: model được đọc từ `/models` sau lần kiểm tra đầu, nên không bị lỗi thời.
- **Lưu kết nối.** Kết nối vẫn là loại `openai_compatible`; cột mới `providers.preset` (migration 00011) chỉ dùng để hiện tên, logo và link lấy key. Preset không hợp lệ, hoặc gắn cho loại kết nối khác, sẽ bị bỏ qua. Export/import mang theo `preset`.
- **Thống kê** (`GET /api/providers/stats?days=7`), tính từ bảng `runs`, không tính các lượt kiểm tra kết nối:
  - Tổng hôm nay: lượt gọi, token, chi phí, lỗi.
  - Mỗi kết nối: lượt gọi, lỗi, token, chi phí, độ trễ trung bình, lần dùng gần nhất, model dùng nhiều nhất, và số lượt theo từng ngày (đủ mọi ngày trong khoảng, theo giờ của office).
- **Giao diện.**
  - Đầu trang là dải tổng hợp; ô "7 ngày" mở trang Chi phí.
  - Mỗi thẻ kết nối có logo chữ cái, trạng thái, 4 chỉ số, biểu đồ cột 7 ngày, và lần dùng gần nhất.
  - Form thêm kết nối: hàng trên là các tài khoản chính; bên dưới là mục mở ra "Nhà cung cấp API khác" chia nhóm, cùng lựa chọn tự nhập địa chỉ.

**Để sau (giai đoạn 2).** Tự chuyển sang kết nối dự phòng khi gặp lỗi 429/5xx hoặc chạm trần (giống "combo" của 9router). Đăng nhập OAuth của các gói thuê bao khác.

---

## ADR-029: MCP nội bộ "office" và nút "Sửa lỗi"

**Bối cảnh.** Agent chỉ đọc được code. Muốn hỏi từ Chat về build, deploy, giám sát, và tự tìm cách xử lý khi có lỗi, agent cần đọc được dữ liệu vận hành thật.

**Quyết định.**
- **Công cụ** (`internal/officetools`, chỉ đọc, luôn giới hạn trong một project):
  - `ops_overview`: tiến trình (trạng thái, mã thoát, cổng), service docker compose, giám sát và sự cố gần đây.
  - `process_logs {name, lines}`.
  - `container_logs {service, lines}`.
  - `monitor_detail {name}`: các lần kiểm tra, sự kiện và phân tích AI trước đó.
- **Claude Code** dùng qua **MCP** (`internal/mcpserver`), endpoint `/mcp` trên chính API của office, chỉ nghe ở máy (127.0.0.1):
  - Giao thức streamable HTTP, trả JSON, không giữ phiên. Hỗ trợ `initialize`, `ping`, `tools/list`, `tools/call`.
  - Mỗi lượt chạy có một bearer token riêng, gắn với project, thu hồi khi lượt chạy xong (tối đa 30 phút).
  - Runner truyền `--mcp-config <file tạm 0600>` (token không nằm trong argv), giữ nguyên `--strict-mcp-config`, và thêm `mcp__office` vào `--allowedTools`.
- **Agent qua API (Anthropic/OpenAI)** nhận cùng bộ công cụ gắn trực tiếp vào vòng gọi tool. Codex chưa có.
- **Prompt hệ thống** hướng dẫn agent dùng các công cụ này khi được hỏi về lỗi build, lỗi chạy, deploy hay giám sát.
- **Nút "Sửa lỗi"** (ở tiến trình lỗi, container lỗi, và sự kiện Down mới nhất của giám sát đang Down):
  - Mở một cuộc trò chuyện mới rồi gửi ngay, kèm log đính kèm (cho runtime không có công cụ) và chỉ dẫn dùng công cụ office.
  - Agent đề xuất diff; người dùng duyệt rồi bấm Chạy lại.
  - Nút "Hỏi agent" vẫn giữ để chỉ điền sẵn câu hỏi, không gửi.

**Đã kiểm tra thật** với Claude Code (Haiku):
- Agent gọi `mcp__office__ops_overview` và trả lời đúng trạng thái.
- Luồng "Sửa lỗi" tìm đúng lỗi cố ý trong `app.js` và tạo diff chờ duyệt.

**Để sau.**
- Công cụ có tác động (chạy lại build/test, restart service), cần người duyệt.
- Công cụ cho Codex.
- Tự chạy lại sau khi diff được duyệt.

---

## ADR-030: Agent đề xuất thao tác vận hành, người duyệt mới chạy

**Bối cảnh.** Sau khi sửa code, cần chạy lại build/test để kiểm chứng, hoặc khởi động lại dịch vụ bị treo. Agent không được tự thao tác.

**Quyết định.**
- **Công cụ `propose_action {action, target, reason}`** có trong MCP office và trong bộ công cụ của agent qua API.
  - Hành động cho phép: `run_process`, `restart_process`, `stop_process` (target là tên tiến trình); `start_container`, `restart_container`, `stop_container` (target là service compose).
  - Kiểm tra lúc đề xuất: hành động phải nằm trong danh sách, mục tiêu phải thuộc project. Nếu đã có một đề xuất giống hệt đang chờ trong cùng cuộc trò chuyện/Việc thì trả lại đề xuất đó, không tạo trùng.
  - Công cụ này không có `readOnlyHint`.
- **Lưu trữ:** bảng `actions` (migration 00012). Mỗi lượt chạy mang theo phạm vi (project, cuộc trò chuyện hoặc Việc, `run_ref`) qua token MCP.
  - Chat: hết lượt, các đề xuất của lượt đó được gắn vào câu trả lời của agent (`message_id`) và gửi sự kiện `action`.
  - Việc: đề xuất gắn theo `task_id`, và chi tiết Việc có danh sách riêng.
- **Duyệt:** `POST /api/actions/:id/approve|reject` (chỉ admin, ghi audit).
  - Duyệt thì office chạy qua `ops`: Start/Restart/Stop với tiến trình, `docker compose up|restart|stop <service>` với container. Kết quả thành `done` hoặc `failed` kèm chi tiết.
  - Đề xuất đã xử lý rồi trả 409.
- **Giao diện:** thẻ đề xuất dưới câu trả lời trong Chat và trong chi tiết Việc, gồm Duyệt/Từ chối, kết quả, và link "Xem log" sang tab Vận hành.
- **Prompt hệ thống:** dặn agent sau khi đề xuất sửa code thì đề xuất chạy lại build/test liên quan để kiểm chứng.

**Đã kiểm tra thật** với Claude Code: agent gọi `propose_action` và thẻ chờ duyệt hiện ra. Duyệt thì build chạy xong với mã thoát 0; duyệt lần hai bị chặn.

---

## ADR-031: Supervisor và "Cập nhật office" từ mã nguồn, tự quay về bản cũ

**Bối cảnh.** Office không tự khởi động lại được từ bên trong. Muốn office dùng được thay đổi của chính nó (do agent hoặc người sửa), cần một tiến trình đứng ngoài để thay bản và cứu khi bản mới hỏng. Máy cài bản build sẵn (không có mã nguồn) sẽ cập nhật bằng bản phát hành, làm sau.

**Quyết định.**
- **Hai lệnh.**
  - `office run` là **supervisor**: chạy `office serve` (API) và dashboard (`node dashboard/.output/server/index.mjs`, cổng 2704) làm tiến trình con, rồi tự bật lại khi chúng dừng. Server dừng liên tục hơn 5 lần trong 1 phút thì supervisor thoát.
  - `office serve` là server như trước. Supervisor tự xử lý `--no-ui`, `--ui-port`, `--ui-dir`, các cờ khác chuyển nguyên cho `serve`.
- **Cập nhật** (`POST /api/system/update`, chỉ admin). Chỉ bật khi server chạy dưới supervisor (biến `OFFICE_SUPERVISED`) và tìm thấy mã nguồn (đi ngược lên từ `bin/office` tới `go.mod` của module office).
  1. Build server ra `bin/office.new`; tùy chọn `go test ./...`; build dashboard ra `.output.new` (Nuxt đọc `OFFICE_UI_OUT_DIR`). Bản đang chạy không bị đụng tới. Build hoặc test lỗi thì dừng và ghi `failed`.
  2. Đổi bản: bản hiện tại thành `*.prev`, bản mới vào đúng chỗ. Server tắt gọn gàng rồi thoát với **mã 75**.
  3. Supervisor khởi động lại cả hai và chờ `/healthz`. Tối đa 25 giây, nhưng thất bại ngay nếu tiến trình mới thoát. Không lên thì `Rollback`: bản `.prev` quay lại chỗ, bản lỗi thành `*.failed`, chạy lại bản cũ và ghi `rolled_back`. Lên được thì ghi `ok`. Kết quả nằm ở `<office>/update.json`.
  - Nếu đang có lượt chat hoặc Việc chạy, trả 409 kèm số lượng; người dùng xác nhận thì mới làm (`force`).
- **Không để lại tiến trình mồ côi.**
  - Vòng lặp supervisor không dùng lệnh chờ chặn, nên luôn xử lý được tín hiệu tắt.
  - Server dưới supervisor tự tắt khi supervisor cha mất.
  - Supervisor ghi PID tiến trình con vào `<office>/supervisor.pids`, và khi khởi động thì dọn các tiến trình con còn sót của lần trước. Chỉ dọn tiến trình trông đúng là `office serve` hoặc dashboard.
- **Giao diện.**
  - Trang **Quản trị → Cập nhật office**: bản đang chạy (version, commit, có thay đổi chưa commit hay không), trạng thái supervisor, kết quả lần trước, nút cập nhật (tùy chọn chạy test), log build trực tiếp; sau khi khởi động lại thì trang tự tải lại.
  - Project trỏ vào mã nguồn office có dòng nhắc kèm nút sang trang này. Project đó vẫn là project bình thường.
- **Tiến trình của project** dừng khi cập nhật; mục bật "bật cùng office" tự chạy lại. Container không bị ảnh hưởng.

**Đã kiểm tra trên bản sao repo:**
- Cập nhật thành công: có PID mới, kết quả `ok`.
- Bản mới crash lúc khởi động: tự quay về bản cũ sau 11 giây (tính cả build), API và dashboard vẫn chạy.
- Kill cứng supervisor: server tự tắt; dashboard còn sót được dọn ở lần chạy sau.

**Để sau.** Cập nhật từ bản phát hành (tag git kèm file build sẵn), và gộp dashboard vào file chạy để phát hành chỉ còn một file.

---

## ADR-032: Quy tắc chia việc và quyền theo gói (agent, chế độ, project)

**Bối cảnh.** Lần chạy thật Việc "dark/light theme + 2 ngôn ngữ" (Tam quyền, $2.39) thất bại: 25 diff, 18 bị phủ quyết, 7 không áp được. Nguyên nhân là người lập kế hoạch không chốt quy ước chung, giao việc chồng file và giao việc quá lớn, và worker sửa ra ngoài phạm vi. Người dùng cũng muốn các chế độ quyền giống Claude Code, với quyền theo gói cho từng agent.

**Quyết định: chia việc.**
- Kế hoạch có thêm:
  - `conventions`: quy ước chung, bắt buộc khi có từ 2 việc trở lên.
  - Mỗi việc có `files` (tối đa 3 file, bắt buộc với việc sửa code), `depends_on` (chỉ trỏ tới việc đứng trước) và `done_when`.
- `validatePlan` từ chối kế hoạch khi: thiếu file, có việc quá 3 file, một file thuộc hai việc, phụ thuộc sai, hoặc thiếu quy ước. Kế hoạch bị từ chối được trả lại cho người lập kế hoạch một lần, kèm danh sách lỗi.
- Việc chạy theo từng đợt phụ thuộc: các việc độc lập chạy song song (tối đa 3), việc phụ thuộc nhận kết quả của việc trước. Worker nhận quy ước chung và danh sách file được sửa.
- Diff sửa file ngoài danh sách được giao bị loại ngay.

**Quyết định: quyền theo gói** (`internal/perm`), gói cao bao gồm gói thấp:
1. `read`: Chỉ đọc.
2. `propose`: Đề xuất, người duyệt.
3. `check`: Tự chạy lệnh kiểm tra được phép.
4. `edit`: Tự áp diff áp được sạch.
5. `operate`: Tự chạy/chạy lại tiến trình và container được phép.

- **Quyền thực tế = min(gói của agent, chế độ của Chat/Việc, gói tối đa của project).**
  - Gói agent lưu trong `permissions.level`; `read_only` cũ được giữ đồng bộ. Agent cũ không có gói thì suy ra `read` hoặc `propose`.
  - Chế độ lưu ở `conversations.mode` và `tasks.mode_level` (migration 00013). Thành viên không phải admin chỉ chọn được tối đa `propose`.
  - Chính sách project (`settings` khóa `policy:<project>`) gồm: `max_level` (mặc định `propose`), `allowed_commands` (id tiến trình), `allowed_containers`, `deny_paths` (glob, hỗ trợ `**/` và thư mục có `/` ở cuối; mặc định là `.env*`, `*.pem`, `*.key`).
- **Chỗ áp dụng:**
  - System prompt nói rõ quyền của agent trong lượt đó.
  - Dưới `propose`: không nhận diff, ẩn `propose_action`.
  - `deny_paths` chặn diff ở mọi gói.
  - Chat ở gói `edit` trở lên: diff áp sạch được tự áp, người quyết định ghi là `auto:<agent> (<gói>)`.
  - Việc: diff của agent gói `edit` chỉ tự áp **khi Việc hoàn tất**. Nếu Giám sát phủ quyết thì diff đã bị từ chối, không áp được nữa.
  - `propose_action` tự thực hiện khi: là job nằm trong `allowed_commands` và agent có gói `check`; là tiến trình/container nằm trong danh sách cho phép và agent có gói `operate`. Ngoài ra thì chờ duyệt.
- **Giao diện:**
  - Chọn gói cho agent trong trình sửa mô hình.
  - Mục **Cấu hình → Quyền** của project.
  - Bộ chọn chế độ trong ô Chat và form giao Việc; các gói vượt trần project bị làm mờ kèm lý do.
  - Thẻ diff/thao tác được quyết định tự động có nhãn "tự động".

**Kết quả thật và chạy lại.**
- Việc chỉ được tính `done` khi Giám sát không đánh giá `fail`, và nếu có diff thì còn ít nhất một diff dùng được (đang chờ hoặc đã áp). Ngược lại là `failed`, kèm lý do, ví dụ "Giám sát đánh giá chưa đạt: …" hoặc "Không có thay đổi code nào dùng được: X bị từ chối, Y không áp được". Dashboard hiện trạng thái này là "Không thành công".
- `POST /api/tasks/:id/retry {learn, mode}` tạo Việc mới với cùng mục tiêu, file đính kèm, trần chi phí và chế độ.
  - `learn` thêm vào prompt cho đội: kết luận lần trước, lý do thất bại, và danh sách diff không dùng được kèm lý do.
- Chi tiết Việc có 2 nút: "Chạy lại, rút kinh nghiệm" và "Chạy lại".

**Đã kiểm tra:**
- Test cho quy tắc chia việc, tính gói và tự duyệt thao tác.
- E2E: tự áp ở chế độ `edit`, chờ duyệt ở `propose`, chặn diff sửa `.env`.

---

## ADR-033: Sửa tới khi xong, chỉ dừng khi không sửa được hoặc cần người quyết định

**Bối cảnh.** Lần chạy lại Việc dark/light theme cho thấy kế hoạch đã đúng hướng, nhưng chỉ vì một lỗi trong file nền (gọi hàm không tồn tại) mà Giám sát phủ quyết cả 11 diff. Người dùng muốn đội tiếp tục sửa khi còn sửa được, và chỉ dừng khi không sửa được hoặc cần họ xác nhận.

**Quyết định.**
- **Giám sát chọn một trong 4 kết luận:**
  - `pass`: đạt.
  - `fix`: còn lỗi sửa được, kèm `fixes: [{job, issue}]`.
  - `ask`: cần người dùng quyết định, kèm `question`.
  - `fail`: không sửa được trong phạm vi này.
- **Vòng lặp** (`reviewLoop`) chạy tới khi đạt:
  - Các việc bị nêu lỗi và các việc có diff không áp được được **làm lại**. Diff cũ đánh dấu "Thay bằng bản sửa ở vòng N".
  - Worker nhận: lỗi cần sửa, bản làm trước, kết quả của các việc nó phụ thuộc, quy ước chung, danh sách file. Worker được dặn đọc lại file hiện tại vì diff cũ chưa được áp.
  - Sau mỗi vòng, Giám sát kiểm tra lại.
- **Điểm dừng:**
  - `pass`: Việc hoàn tất.
  - `fail`: phủ quyết, Việc "Không thành công".
  - `ask`: Việc chuyển trạng thái **`needs_input`**, hiển thị "Chờ bạn trả lời" (migration 00014 dựng lại bảng `tasks` để thêm trạng thái này).
  - Chạm trần chi phí của Việc hoặc của ngày.
  - Chốt chặn cuối: cùng một nhóm lỗi lặp lại 2 vòng liên tiếp (không tiến triển), hoặc đủ 8 vòng.
- **Mô hình Team** (không có Giám sát): diff không áp được được gửi lại cho worker, cho tới khi áp được hoặc không còn tiến triển.
- **Trả lời câu hỏi:** `POST /api/tasks/:id/retry {answer}` chạy tiếp Việc, kèm câu hỏi và câu trả lời (cùng bài học của lần trước).
- **Giao diện:**
  - Mỗi lần kiểm tra hiện kết luận (Đạt / Cần sửa / Cần hỏi bạn / Không sửa được), số vòng, và danh sách "Việc N cần sửa: …".
  - Việc chờ trả lời có ô trả lời và nút "Trả lời và chạy tiếp".

**Chờ duyệt và duyệt cả lô.**
- Việc `done` mà còn diff `pending` thì hiện **"Chờ duyệt (n)"**: DTO của Việc có thêm `pending_patches` và `applied_patches`.
- `POST /api/tasks/:id/patches/approve-all` (admin):
  - Gộp mọi diff đang chờ thành một patch, chạy `git apply --check` cho cả lô, đạt mới áp.
  - Có diff hỏng thì **không áp gì**, và báo diff nào không áp được.
- `POST /api/tasks/:id/patches/revert-all` gỡ cả lô bằng `git apply -R`, theo thứ tự ngược. Các diff được đánh dấu "Đã hoàn tác cả lô".
- `git apply` giờ tự thêm dòng `diff --git` trước từng file khi diff thiếu. Không có dòng này, cờ `--recount` đọc nhầm dòng `--- a/x` của file sau thành dòng bị xóa của hunk trước, nên diff nhiều file của agent bị báo "không áp được" oan. File mới (`--- /dev/null`) được thêm `new file mode`, file bị xóa được thêm `deleted file mode`.
- **Giao diện:**
  - Nút "Duyệt tất cả (n)" và "Hoàn tác cả lô".
  - Áp xong thì gợi ý chạy các lệnh kiểm tra (tiến trình loại job) của project.
  - Các bản cũ đã được thay bằng bản sửa được gom vào mục thu gọn.

**Đã kiểm tra (test):**
- Sửa rồi đạt: 2 lần kiểm tra, 2 lần làm; diff cũ bị thay, diff mới đang chờ duyệt.
- Hỏi người dùng rồi chạy tiếp sau khi được trả lời.
- Dừng khi lỗi lặp lại.

## ADR-034: Quyền git cho agent và trao đổi tiếp với quản lý sau Việc

**Git.**
- Công cụ đọc: `git_status`, `git_diff`, `git_log` (office MCP và công cụ của agent API).
- Thao tác đi qua `propose_action`: `git_commit` (message và file), `git_branch`, `git_push`.
- Trước khi ghi nhận, office kiểm tra:
  - commit phải có message;
  - chỉ gồm file đang thay đổi;
  - không đụng file cấm.
- Lưu ở `actions.args` (migration 00015).
- Tự làm theo gói quyền:
  - commit: từ gói Tự sửa code;
  - tạo nhánh: gói Vận hành;
  - **push luôn cần người duyệt**, không bao giờ `--force`.
- Trên dashboard, nút **Commit** của Việc gọi `POST /api/tasks/:id/commit-draft`. API này lấy các file đã áp của Việc mà git còn thấy thay đổi, rồi nhờ model nhanh soạn message theo phong cách `git log` của repo. Người dùng sửa message, chọn file, rồi commit qua `POST /api/projects/:id/git/commit` và push qua `/git/push` (admin). Hai lệnh này dùng chung đường kiểm tra và lịch sử của actions.

**Trao đổi với quản lý.**
- Mỗi Việc có một cuộc trò chuyện riêng: `conversations.task_id`, migration 00016, duy nhất theo Việc.
- Người trả lời là agent đã lập kế hoạch cho Việc. Nếu không có thì là lead đầu tiên.
- Chế độ quyền mặc định lấy theo chế độ của Việc.
- Mỗi lượt, system prompt được dựng lại từ trạng thái mới nhất của Việc: yêu cầu, kết quả, 16 bước gần nhất, diff và thao tác. Nhờ vậy quản lý thấy cả những gì đã duyệt, hoàn tác hay commit sau đó.
- Diff và thao tác trong cuộc trao đổi được gắn `task_id`, nên hiện trong Việc và nằm trong "Duyệt tất cả".
- Cuộc trao đổi của Việc không hiện trong danh sách Chat của project.

## ADR-035: Quyền lẻ, gói là preset, lệnh theo gói lệnh

**Bối cảnh.** senprints-agents (chỉ tham khảo) để quyền ở 4–5 chỗ tách rời: allow list MCP, skill, danh sách thu hồi built-in, strict mode và permission_mode. Bash chỉ bật/tắt toàn bộ, không có preset, nên khó dùng. Ở đây mỗi agent chỉ có một chỗ chỉnh quyền.

**Quyết định.**
- **Quyền lẻ** (`internal/perm/caps.go`):
  - `propose`
  - `code.apply`
  - `commands.run`
  - `git.commit`
  - `git.branch`
  - `ops.process`
  - `ops.container`

  Mỗi quyền có mức thấp nhất chứa nó. Push không phải một quyền: push luôn cần người duyệt.
- **Gói** (5 mức cũ) giờ là preset của các quyền lẻ.
  - Agent chọn gói, rồi có thể bật/tắt từng quyền. Khi đó `permissions.caps` được lưu. Nếu lựa chọn trùng một gói thì tự quay về gói đó.
  - Mức riêng của agent là mức cao nhất trong các quyền đã chọn.
  - Chế độ chat/Việc và gói tối đa của project vẫn hạ thấp theo mức: quyền nào có mức cao hơn thì bị tắt trong lượt đó.
- **Lệnh**:
  - Project bật từng lệnh (`policy.commands`), chọn từ các **gói lệnh**:
    - gói có sẵn theo thư mục: Git đọc, Go, Node, Python, Rust, Docker đọc;
    - gói script sinh từ `package.json` theo package manager;
    - gói do project tự tạo.
  - Agent có thể thu hẹp danh sách lệnh (`permissions.commands`; không đặt = mọi lệnh của project).
  - Mẫu lệnh là dòng lệnh; `" *"` ở cuối nghĩa là kèm tham số tùy ý.
  - Lệnh chạy **không qua shell**: từ chối `| ; & < > $` và backtick. Chạy trong thư mục project, tối đa 5 phút, giữ 6KB output cuối.
- **Công cụ `run_command`** (office MCP và agent API):
  - Lệnh có trong danh sách và agent có quyền `commands.run` thì chạy ngay, trả output cho agent.
  - Lệnh khác thành action `run_command` chờ duyệt. Duyệt xong thì chạy và lưu output.
- Tự làm của actions giờ xét theo quyền lẻ (`Scope.Access`), không còn theo mức. Tự áp diff trong chat/Việc cũng vậy.

**Giao diện.**
- Agent (Mô hình → agent): thanh chọn gói, 4 nhóm quyền (Code / Lệnh / Git / Vận hành) bật tắt bằng switch, nhãn "Tùy chỉnh" kèm nút "Về gói X", và cây gói lệnh có checkbox ba trạng thái để chọn từng lệnh.
- Project (Cấu hình → Quyền): gói tối đa kèm dòng tóm tắt "được tự làm tối đa", thẻ gói lệnh (bật cả gói hoặc từng lệnh, thêm gói hay lệnh riêng), tiến trình, container và file cấm.

## ADR-036: Claude Code mặc định dùng cấu hình như CLI của người dùng

**Bối cảnh.** ADR-022 cô lập Claude Code khỏi cấu hình cá nhân. Hệ quả là agent không dùng được MCP (Jira, Figma, docs…) và skill mà người dùng vẫn dùng khi tự chạy `claude` trong repo. Người dùng muốn agent làm việc giống như họ chạy CLI.

**Quyết định.**
- Mặc định (`policy.isolate_claude = false`):
  - `--setting-sources user,project,local`: nạp `~/.claude`, `.mcp.json`, plugin, hook và skill như CLI.
  - Thêm tool `Skill` vào `--tools`.
  - Không còn `--strict-mcp-config`. MCP nội bộ office vẫn được thêm qua `--mcp-config`.
- **Luôn `--permission-mode dontAsk`**, ở cả hai chế độ. Chỉ các tool được allow mới chạy, gồm tool mặc định của office và `permissions.allow` trong settings của người dùng/repo. Mọi tool khác bị từ chối chứ không hỏi. Nhờ vậy `defaultMode: auto` hay `bypassPermissions` của người dùng không làm agent tự gọi MCP ghi dữ liệu.
- Tool dựng sẵn vẫn chỉ gồm `Read`, `Glob`, `Grep` (và `Skill`). Sửa code vẫn đi qua diff được duyệt, lệnh vẫn qua `run_command` (ADR-022, ADR-035).
- ~~Project bật "Tách Claude Code khỏi cấu hình của bạn" thì quay về cách của ADR-022.~~ Đã bỏ tùy chọn này. Người dùng không có trường hợp nào cần tách, nên agent luôn dùng cấu hình như CLI.
- Chỉ áp dụng cho kết nối Claude Code. Codex và agent API không đổi.

**Đánh đổi.** Một lượt nhỏ đắt hơn (đo với haiku: ~$0.09 so với ~$0.023 khi cô lập), vì phải nạp danh sách MCP và skill. Hook của người dùng cũng chạy.

## ADR-037: Agent sửa code trong git worktree riêng, hoặc sửa thẳng như CLI

**Bối cảnh.** Agent chỉ đọc và viết diff bằng chữ (ADR-022), nên không tự áp hay chạy build/test trên bản sửa của mình được. Diff tự viết hay không áp được: lần Việc Tam quyền $2.39 có 7 diff hỏng. Vòng sửa (ADR-033) cũng không thấy file đã sửa. Các công cụ khác (Claude Code `--worktree`, Codex cloud, Cursor background agents, Copilot coding agent) đều cho agent làm trong bản sao riêng rồi mới đưa người duyệt. Người dùng muốn theo cách đó, và thêm một lựa chọn sửa thẳng như khi họ chạy CLI.

**Quyết định.**
- `policy.edit_mode` của project (Cấu hình → Quyền, chỉ admin):
  - `worktree` (mặc định).
  - `direct`: sửa thẳng.
- **Worktree** (`internal/worktree`):
  - Mỗi Chat có `chat-<id>`, mỗi Việc có `task-<id>`, đặt tại `<office home>/worktrees/<project>/`. Tạo bằng `git worktree add --detach` từ một snapshot của project. Snapshot gồm cả code chưa commit và file mới. Nó được dựng qua index tạm (`GIT_INDEX_FILE`, `write-tree`, `commit-tree`), nên không đụng index, file hay nhánh của người dùng.
  - Các thư mục `node_modules`, `.venv`, `venv` bị gitignore được symlink sang worktree. Các file `.env*` bị gitignore được sao chép. Thư mục khác thêm qua `policy.worktree_links`. Symlink được ghi vào `info/exclude`, vì mẫu `node_modules/` chỉ khớp thư mục chứ không khớp symlink.
  - HEAD của worktree là "điểm đã gộp". Diff = `git add -A` + `git diff --cached --binary HEAD`, nên luôn áp được vào điểm xuất phát.
- **Công cụ ghi** cho lượt được sửa (agent từ gói `propose`):
  - Claude Code: `Edit`, `Write`. File bí mật, `.git` và `deny_paths` bị chặn bằng `--disallowedTools`, vẫn ở chế độ `dontAsk` (ADR-036).
  - Agent API: `write_file`, `edit_file` (thay đúng một chỗ), cũng chặn file bí mật và file cấm.
  - Codex: `--sandbox workspace-write`.
  - `run_command` chạy trong worktree. Các lệnh project đã bật được **tự chạy từ gói `propose`**, vì trong worktree chưa gì chạm tới project. Lệnh khác vẫn chờ duyệt.
  - Commit, tạo nhánh và push bị từ chối trong worktree: làm sau khi gộp.
- **Chat:**
  - Sau mỗi lượt, mọi thay đổi chưa gộp thành một diff (`patches.origin = 'worktree'`, migration 00017). Diff cũ đang chờ được đánh dấu "Thay bằng thay đổi mới hơn".
  - **Gộp** = `git apply` vào thư mục project (chưa commit), rồi `worktree.Accept` dời HEAD của worktree qua diff đó.
  - **Từ chối** = `git apply -R` trong worktree.
  - Gói có `code.apply` thì tự gộp.
  - Xóa cuộc trò chuyện thì xóa worktree.
- **Việc:**
  - Các worker của một Việc dùng chung một worktree. Kế hoạch đã chia file không chồng nhau (ADR-032).
  - Chỉ bước `work` được ghi. Các bước lập kế hoạch, bỏ phiếu, kiểm tra và tổng hợp đọc worktree, nên Giám sát thấy code thật.
  - Diff của mỗi việc là thay đổi cộng dồn trên đúng các file được giao, nên bản sửa ở vòng sau thay bản cũ. Sửa file không ai được giao (so với trước bước đó) hoặc file cấm thì bị trả lại như cũ trong worktree và ghi thành diff "không dùng được", để việc đó được làm lại.
  - Việc kết thúc thì xóa worktree: diff đã lưu, và áp vào project không cần worktree.
- **Sửa thẳng:**
  - Agent làm ngay trong thư mục project với cùng bộ công cụ ghi, không có diff và không cần duyệt.
  - Mỗi project chỉ một lượt chat sửa thẳng tại một thời điểm.
- **Khi không dùng được worktree** (không phải git repo): quay về cách của ADR-022, tức là đọc và viết diff bằng chữ.
- **Dọn dẹp:** khi khởi động, xóa worktree của Việc, của cuộc trò chuyện đã xóa, và worktree không đổi trong 14 ngày.

**Giới hạn.**
- Worktree không tự cập nhật khi code ở project đổi sau lúc tạo. Nếu diff không còn áp được thì thẻ báo lỗi, người dùng nhờ agent làm lại.
- Lệnh kiểm tra chỉ tự chạy khi project đã bật gói lệnh.

**Đơn giản hóa trang Quyền (cùng đợt).**
- Bỏ "gói tối đa" của project. `policy.max_level` luôn là `operate`, nên giới hạn thực tế chỉ còn là min(gói của agent, chế độ chọn ở Chat/Việc). Thành viên không phải admin vẫn chỉ chọn được tối đa `propose`.
- Trang Quyền thành các thẻ ngắn: Sửa code ở đâu, Lệnh được tự chạy, Tiến trình & container, File cấm sửa (dạng chip), và mục "Nâng cao" thu gọn (mang thêm vào worktree, tách Claude Code). Phần giải thích chuyển vào tooltip ⓘ.

## ADR-038: Project liệt kê, agent được cấp quyền

**Bối cảnh.** Trang Quyền của project có ô tick lệnh, tiến trình và container. Agent cũng có ô tick lệnh riêng, nên người dùng không rõ quyền thật nằm ở đâu.

**Quyết định.**
- **Project** (Cấu hình → Quyền) chỉ nêu những gì repo có và những gì cấm mọi agent:
  - Sửa code ở đâu.
  - **Danh mục lệnh**: gói lệnh phát hiện được, cộng gói tự tạo. Không có ô tick.
  - File cấm sửa.
  - Nâng cao.
  - Bỏ `policy.commands`, `allowed_commands`, `allowed_containers`.
- **Agent** (Mô hình → agent) được cấp quyền: gói quyền, quyền lẻ, cộng các lựa chọn sau.
  - `permissions.commands`: lệnh chọn từ danh mục. Không đặt thì là **lệnh an toàn**:
    - các gói có sẵn, gồm Git đọc, Docker đọc, test/lint/build của Go, Node, Python, Rust, nhưng không có `go mod tidy`;
    - các script `test`, `lint`, `typecheck`, `build`, `check`… trong package.json.
    - Gói tự tạo không bao giờ là mặc định.
  - `permissions.processes`: tiến trình được tự chạy lại. Không đặt thì là các tiến trình kiểm tra (loại job).
  - `permissions.containers`: không đặt thì không có container nào.
- **Lệnh an toàn tự chạy từ mức Chỉ đọc**, ở cả thư mục project lẫn worktree.
  - Ở mức Chỉ đọc, `run_command` chỉ chạy lệnh an toàn. Lệnh khác bị từ chối, không tạo đề xuất.
  - Lệnh khác mà agent được chọn: tự chạy khi agent có quyền `commands.run`, hoặc khi ở trong worktree với mức `propose`. Ngoài ra thì chờ duyệt.
- `perm.LoadPolicy` tính danh mục, danh sách lệnh an toàn và các job của project (`Catalog`, `Safe`, `Jobs`; không lưu).
- **Sửa lỗi kèm theo:** trước đây `Access.Commands` chỉ được tính khi agent có `commands.run`. Vì vậy agent mức Đề xuất trong worktree thực tế không tự chạy được lệnh nào (ADR-037).

**Cập nhật: "Sửa code ở đâu" chọn theo từng Chat/Việc (thay cho `policy.edit_mode`).**
- Lưu ở `conversations.edit_mode` và `tasks.edit_mode` (migration 00019, mặc định `worktree`).
- Dashboard có bộ chọn cạnh bộ chọn quyền trong ô Chat và form giao Việc.
- Chỉ admin chọn được "Sửa thẳng". Chạy lại một Việc thì giữ cách sửa code cũ.
- Trang Quyền của project không còn mục này.
- Bỏ thanh tab Chat / Việc / Vận hành / Cấu hình trong trang project, vì sidebar đã có các mục đó. Bấm vào project thì mở Chat.

## ADR-039: Trang chi tiết agent

**Bối cảnh.** Agent chỉ được sửa trong một panel trượt của trình sửa mô hình, và không có chỗ nào xem agent đã làm gì, tốn bao nhiêu hay đã bị sửa những gì. Đã tham khảo trang agent của senprints-agents (chỉ lấy ý tưởng, không copy code). Những điểm cố ý không lặp lại:
- form khoảng 50 trường trên một trang, một nút Lưu;
- phiên bản chỉ là JSON thô, không so sánh được, không khôi phục được;
- lượt chạy, thống kê và log nằm rải ở ba nơi;
- chat thử đi theo đường code khác với khi chạy thật.

**Quyết định.**
- Trang `/projects/:id/agents/:agentId`. Bấm vào thẻ agent trong sơ đồ mô hình của project là mở trang này. Mô hình mẫu trong thư viện vẫn dùng panel cũ.
- Đầu trang: project và mô hình, cấp bậc, model thực dùng, gói quyền, và nút **Chat với agent**. Nút này mở một cuộc Chat thật trong project, nên chạy thử và chạy thật dùng chung một đường code.
- Tab **Tổng quan**, chọn khoảng 7, 30 hoặc 90 ngày (`GET /api/agents/:id/stats?days=`):
  - 5 thẻ số: lượt chạy, tỉ lệ thành công, chi phí (kèm số lượt không rõ giá), thời gian p95 (kèm trung vị), số diff được gộp trên tổng số diff.
  - Biểu đồ lượt/ngày tách thành công và lỗi, có hover và xem dạng bảng.
  - Chi phí theo model.
  - 5 loại lỗi hay gặp nhất, gom theo dòng đầu của thông báo lỗi.
  - Số liệu lấy từ bảng `runs` (lọc theo `agent_id`) và các diff trong Chat/Việc của agent.
- Tab **Cấu hình** chia 3 thẻ, mỗi thẻ có nút Lưu riêng: Vai trò & hướng dẫn, Model & kết nối AI, Quyền (dùng lại `AgentPermEditor`).
- Tab **Hoạt động** (`/activity`) là một dòng thời gian gộp:
  - các cuộc Chat với agent;
  - các Việc agent tham gia, kèm số bước, các phase và chi phí.
  - Bấm vào một mục là mở đúng cuộc Chat hoặc đúng Việc đó.
- Tab **Lịch sử** (`/history`) dựng lịch sử riêng của agent từ các snapshot mô hình đã có (ADR-019):
  - Mỗi snapshot là trạng thái ngay trước một lần sửa. Trạng thái ngay sau là snapshot mới hơn kế tiếp, hoặc agent hiện tại nếu là lần sửa mới nhất.
  - So sánh 11 trường. Hướng dẫn và mô tả được so theo từng dòng. Nếu agent bị đổi key, lịch sử vẫn theo được.
  - Nút **Quay về trước lần sửa này** (`POST /restore`) chỉ khôi phục agent đó, không khôi phục cả mô hình. Id và key của agent giữ nguyên. Thao tác lưu qua `SaveAgent`, nên tự tạo snapshot và có thể hoàn tác.
- Code nằm trong `internal/agentinfo`, có test cho lịch sử, khôi phục (cả trường hợp đổi key) và thống kê.

**Chưa làm:** kho kiến thức, lịch chạy/webhook, thông báo. Office chưa có các tính năng này.

## ADR-040: Tự động: lịch chạy và trigger gọi agent

**Bối cảnh.** Người dùng muốn agent tự chạy theo lịch (mỗi 5 phút, 8 giờ sáng các ngày trong tuần) và khi có trigger từ API, webhook, Discord hay Telegram. Thường là gọi trưởng nhóm hoặc một agent cá nhân. Đã tham khảo senprints-agents (chỉ lấy ý tưởng).
- Những điểm học theo:
  - hàng đợi nằm trong DB, một cột `next_attempt_at` dùng chung cho retry, debounce và hẹn giờ;
  - mỗi lượt chạy chụp lại prompt, agent và quyền lúc tạo;
  - secret chỉ lưu dạng hash;
  - prompt mẫu chỉ có placeholder cố định;
  - có giới hạn số lượt mỗi giờ và trần chi phí mỗi ngày; tự tắt khi lỗi liên tiếp, lưu mã lý do;
  - kiểm tra cron lúc lưu, nhúng sẵn dữ liệu múi giờ;
  - lịch chạy không chạy chồng;
  - webhook trả ngay trạng thái rõ ràng.
- Những điểm cố ý tránh:
  - chống trùng chỉ giữ trong bộ nhớ;
  - debounce không có hạn chờ tối đa;
  - mỗi nguồn tạo job một kiểu;
  - nhiều đường gửi trả lời Discord khác nhau.

**Quyết định.**

*Mô hình dữ liệu* (migration 00020):
- `automations`: một tự động hóa của project.
  - `id`, `project_id`, `name`, `enabled`, `source` (`schedule` | `webhook` | `telegram` | `discord`).
  - `config` (JSON theo nguồn):
    - schedule: `every_minutes` hoặc `cron`, cùng `timezone`;
    - webhook: `auth` (`bearer` | `header` | `query`), `auth_name`, `secret_hash`;
    - telegram/discord: `channel_id`, `chat_id`, `allow_users[]`, `mention_only`.
  - `action`: `chat` (gửi tin cho một agent) hoặc `task` (giao Việc cho cả đội).
  - `agent_id` (chat; bỏ trống là trưởng nhóm đầu tiên), `prompt` (mẫu), `edit_mode`, `keep_context`.
  - `limits` (JSON): `max_runs_per_hour`, `daily_cost_usd`, `disable_after_failures` (mặc định 5), `debounce_seconds`, `debounce_key`, `debounce_max_seconds`.
  - Trạng thái: `failures`, `disabled_code`, `disabled_reason`, `last_run_at`, `next_run_at`.
  - `created_by`, `created_at`, `updated_at`.
- **`jobs`**: sổ ghi **mọi lần chạy**, gồm lượt Chat do người gửi, Việc do người giao, và lượt do tự động hóa tạo ra. Đây cũng là hàng đợi. Chat (`conversations`, `messages`) và Việc (`tasks`, `task_steps`) vẫn là nơi chứa nội dung. Cách tách "nội dung / lần chạy" này giống Thread/Run của OpenAI Assistants và LangGraph, còn hàng đợi nằm trong DB theo kiểu Oban hay River (ADR-004).
  - Định danh và nguồn gốc:
    - `id`, `project_id`, `kind` (`chat_turn` | `task`);
    - `origin` (`user` | `automation` | `monitor` | `retry`), `origin_id` (id tự động hóa, id giám sát, hoặc job gốc);
    - `trigger` (`ui` | `schedule` | `webhook` | `telegram` | `discord` | `manual`);
    - `created_by`.
  - Liên kết tới nội dung: `conversation_id` + `message_id` (chat_turn), hoặc `task_id` (task).
  - Trạng thái: `status` (`pending` | `running` | `done` | `failed` | `cancelled` | `skipped` | `needs_input`), `error`, `error_code` (mã ổn định để thống kê và dịch, ví dụ `busy_timeout`, `budget`, `rate_limit`, `agent_missing`, `restart`, `agent_error`).
  - Chép sẵn để lọc và thống kê nhanh, không phải join: `agent_id`, `title`, `cost_usd`, `input_tokens`, `output_tokens`, `duration_ms`. Các số này được tính lúc job kết thúc, từ các lượt gọi AI của job.
  - Hàng đợi: `next_attempt_at`, `dedupe_key` (unique theo `origin_id`), `debounce_key`, `debounce_until`, `payload` (tối đa 64KB), `reply` (JSON: kênh, chat, id tin để trả lời).
  - Thời gian: `created_at`, `started_at`, `finished_at`.
  - Index:
    - `(project_id, created_at)`;
    - `(status, next_attempt_at)` cho hàng đợi;
    - `(origin, origin_id, created_at)` cho lịch sử của từng tự động hóa;
    - `(kind, created_at)`, `(agent_id, created_at)`.
- **`runs.job_id`** (lượt gọi AI, ADR-020) trỏ về job, nên chi phí và token của từng job được tính chính xác, Việc có nhiều bước cũng vậy. Trang Chi phí và trang agent lọc được theo nguồn: người hay tự động hóa.
- Cách mỗi loại tạo job:
  - Chat: người gửi tin thì tạo job `chat_turn` (`origin=user`, `trigger=ui`), chạy ngay.
  - Việc: giao Việc thì tạo job `task`. Nếu project đang có Việc chạy, job **xếp hàng** (`pending`) chứ không báo lỗi như trước.
  - Tự động hóa: tạo job `pending`, và nội dung được tạo lúc job bắt đầu chạy. Mặc định là Việc; hành động "gửi tin cho một agent" tạo một lượt Chat.
- Prompt được điền lúc job bắt đầu chạy, từ tự động hóa và `payload`. Sau đó prompt nằm trong tin nhắn hoặc trong mục tiêu của Việc, không lưu lặp lại.

*Chạy* (package `internal/trigger`):
- **Bộ lập lịch** chạy mỗi 15 giây:
  - Tính lượt đến hạn từ `next_run_at`, và tính lại sau mỗi lần tạo lượt, nên khởi động lại không bị chạy sớm.
  - Lỡ nhiều lượt thì chỉ chạy bù 1 lượt, sau đó theo giờ thật.
  - Còn job `pending` hoặc `running` của cùng tự động hóa thì bỏ qua (không chạy chồng).
  - Cron 5 trường (`robfig/cron/v3`), múi giờ IANA, nhúng `time/tzdata`.
- **Bộ chạy:**
  - Lấy job `pending` có `next_attempt_at <= now` bằng một câu `UPDATE … RETURNING` trong transaction.
  - Tối đa 2 lượt chạy cùng lúc trong office, và 1 lượt cho mỗi tự động hóa.
  - `chat`:
    - Mở cuộc Chat với agent. Với `keep_context`, hoặc khi đến từ cùng thread/chat Discord hay Telegram, thì dùng lại một cuộc Chat.
    - Gửi prompt đã điền, đợi lượt trả lời xong, rồi lấy câu trả lời và chi phí.
  - `task`: gọi `tasks.Start`, rồi đợi Việc kết thúc.
  - Chế độ quyền là `operate`, tức không đặt trần thêm: agent làm đúng theo quyền đã phân cho nó (ADR-035). Cách sửa code lấy theo tự động hóa, mặc định là worktree.
  - Người thực hiện ghi là `auto:<tên tự động hóa>`.
- Prompt được điền lúc bắt đầu chạy, từ tự động hóa và `payload`. Sau đó prompt nằm trong tin nhắn của Chat hoặc trong mục tiêu của Việc.
- **Lỗi và giới hạn:**
  - Cuộc Chat đang bận, hoặc project đang chạy một Việc khác: hẹn lại sau 60 giây, quá 30 phút thì `failed`.
  - Lỗi của agent không tự chạy lại, để tránh sửa code hai lần. Mỗi lần lỗi cộng `failures`; đủ `disable_after_failures` lần liên tiếp thì tắt, ghi `disabled_code=failures`.
  - Chạm trần chi phí trong ngày thì tắt với `disabled_code=daily_cost`. Vượt số lượt mỗi giờ thì lượt đó `skipped`.
  - "Chạy ngay" trên dashboard bỏ qua giới hạn.
  - Khởi động lại office: job đang `running` chuyển sang `failed` (`error_code=restart`), không chạy lại. Người dùng bấm "Chạy lại" thì tạo job mới với `origin=retry`.
- **Prompt mẫu.** Các placeholder cố định, không có vòng lặp hay điều kiện vì payload không đáng tin:
  - `{{payload}}`, `{{payload.a.b.0}}`;
  - `{{message}}`, `{{user}}` (cho chat);
  - `{{now}}`, `{{today}}`, `{{yesterday}}` (theo múi giờ);
  - `{{source}}`, `{{automation}}`.
  - Placeholder không biết thì giữ nguyên. Đường dẫn không có trong payload thì để trống.
  - Payload luôn được đánh dấu là dữ liệu chứ không phải lệnh.

*Webhook/API* (giai đoạn 1):
- `POST /hooks/{automation_id}` với token:
  - `Authorization: Bearer …`, header tự đặt tên, hoặc `?name=`;
  - so sánh hash SHA-256 theo thời gian không đổi;
  - không có token, hoặc tự động hóa đã tắt, đều trả 404.
- Chống trùng:
  - lấy từ header `Idempotency-Key`, `X-Request-Id` hoặc id của lần gửi;
  - nếu không có thì dùng hash của body trong 10 phút;
  - lưu trong DB (unique `(origin_id, dedupe_key)`).
- Debounce:
  - gom theo `debounce_key` (đường dẫn trong payload);
  - mỗi lần gửi mới dời `next_attempt_at` và thay payload;
  - không quá `debounce_max_seconds` tính từ lần gửi đầu.
- Kết quả trả về:
  - `202 {status: queued|debounced, job_id}`;
  - `200 {status: duplicate}`;
  - `429` khi vượt giới hạn.
- `GET /hooks/{id}/jobs/{job_id}` (cùng token) trả trạng thái và kết quả.
- Đường đi: dashboard (cổng 2704) chuyển tiếp `/hooks/**` giống như `/api/**`. Muốn nhận từ bên ngoài thì cần URL công khai, ví dụ Cloudflare Tunnel.
- Secret chỉ hiện một lần lúc tạo hoặc đổi. Đổi secret thì secret cũ hết hiệu lực ngay.

*Discord/Telegram hai chiều* (giai đoạn 2 là Telegram, giai đoạn 3 là Discord):
- Bảng `channels` (id, kind, name, token mã hóa AES-GCM như key AI) được quản lý ở trang **Kết nối kênh**.
- Telegram dùng long polling `getUpdates`. Discord dùng Gateway qua thư viện `discordgo`. Không cần URL công khai, cũng không cần chạy bot riêng.
- Nhận tin khi thỏa cả hai điều kiện:
  - tin đến từ kênh hoặc chat đã gắn, và người gửi nằm trong `allow_users` (nếu có đặt);
  - bot được tag, hoặc `mention_only=false`.
- Luồng trả lời:
  1. Bot trả lời ngay "⏳ đang xử lý…".
  2. Xong thì sửa tin đó bằng kết quả. Dài quá thì chia nhỏ (Discord 2000, Telegram 4096 ký tự).
  3. Nếu có diff hoặc thao tác chờ duyệt, gửi kèm link tới dashboard.
- Đánh dấu đã trả lời chỉ sau khi gửi thành công (ít nhất một lần). Trả lời trong cùng thread thì tiếp tục cùng một cuộc Chat.

*API cho dashboard:*
- `GET/POST /api/projects/:id/automations`
- `GET/PATCH/DELETE /api/automations/:id`
- `POST /api/automations/:id/run` (chạy ngay)
- `POST /api/automations/:id/rotate-secret`
- `GET /api/jobs?project=&kind=&origin=&origin_id=&status=&agent=&since=` (phân trang theo con trỏ), `GET /api/jobs/:id`, `POST /api/jobs/:id/cancel`, `POST /api/jobs/:id/retry`
- `GET /api/jobs/stats?project=&since=&by=day|kind|origin|agent|status`: số job, tỉ lệ thành công, chi phí, p50/p95 thời gian
- `GET /api/automations/preview-schedule?cron=&tz=` (5 lần chạy tới, kèm mô tả dễ đọc)
- Tạo, sửa và xóa cần quyền admin.

*Giao diện:*
- Mục **Tự động** trong menu project, đặt sau Việc.
- Trang danh sách: tên, nguồn, lịch hoặc URL, hành động → agent, lần chạy gần nhất, công tắc bật/tắt, nút chạy ngay.
- Tạo và sửa trong panel trượt, chia theo từng bước:
  1. Nguồn: có mẫu cron "mỗi 5 phút", "8:00 T2–T6", "mỗi giờ", kèm xem trước lần chạy tới.
  2. Hành động và agent.
  3. Prompt: có danh sách placeholder.
  4. Giới hạn.
- Với webhook: hiện URL, ví dụ curl, nút đổi secret; secret mới hiện trong hộp thoại chỉ đóng được bằng nút.
- Trang con `/projects/:id/automations/:aid` (tô sáng mục Tự động): lịch sử chạy (thời gian, nguồn, trạng thái, chi phí, mở Chat/Việc), cùng banner khi bị tự tắt, ghi rõ lý do và nút bật lại.

- **Trang Job** (mục chung trong sidebar, và lọc sẵn theo project khi mở từ project):
  - bảng các job, lọc theo loại (Chat, Việc), nguồn (người, tự động hóa nào), trạng thái, agent, thời gian;
  - dòng đầu là thẻ số: đang chạy, đang chờ, lỗi 24 giờ qua, chi phí;
  - mỗi dòng có nút mở (tới Chat hoặc Việc), dừng, chạy lại;
  - job đang chạy cập nhật trực tiếp.

**Giai đoạn.**
1. Bảng `jobs` và hàng đợi (Chat và Việc hiện có chuyển sang tạo job), lịch chạy, webhook/API, trang Tự động, trang Job bản đầu. Làm trước.
2. Telegram hai chiều.
3. Discord hai chiều.

**Phương án đã loại.**
- Mỗi nguồn một hệ thống riêng: tạo job mỗi nơi một kiểu, đã gây lỗi ở senprints-agents.
- Dùng công cụ ngoài như n8n hay Zapier: agent không dùng được quyền, worktree và chi phí của office.
- Discord qua Interactions endpoint: cần URL công khai, và token của interaction hết hạn sau 15 phút.

**Đã làm (giai đoạn 1).**
- Code: `internal/trigger` (lịch chạy, bộ chạy, webhook), `internal/api/automations.go` và `jobs.go`, migration 00020.
- Dashboard: mục Tự động của project, trang Job.
- Khác với spec ở mấy điểm:
  - Job xong thì lấy ngay job kế tiếp, không đợi lượt kiểm tra 15 giây.
  - Chạy lại một lượt tự động tạo job mới với `trigger=manual`, `origin` vẫn là `automation` để giới hạn và lịch sử gom đúng.
  - Đường `GET /api/jobs` cũ (Việc gần đây ở trang Tổng quan) chuyển sang `/api/tasks/recent`.
  - `/hooks/` không đi qua middleware CSRF, vì không dùng cookie mà mỗi tự động hóa có token riêng.
  - Debounce không có khóa thì mọi lần gửi gom thành một lượt.

## ADR-041: Tự động hóa chạy code, gọi AI khi cần, tạo bằng trò chuyện

**Bối cảnh.** Mỗi lượt tự động gọi agent tốn khoảng $1 (ADR-040), vì trưởng nhóm dùng Opus và nạp đủ cấu hình Claude Code. Phần lớn việc định kỳ chỉ cần code: kiểm tra log, gọi API, đếm lỗi. Người dùng muốn:
- job chạy code, không tốn token AI;
- chỉ gọi AI khi có chuyện;
- tạo job bằng cách trò chuyện với agent.

**Quyết định.**
- **Hành động `script`**, cạnh `chat` và `task`. Tự động hóa có thêm `script {lang: bash|node|python, body (≤64KB), timeout_s (mặc định 300, tối đa 3600)}`.
  - Office ghi script ra file tạm rồi chạy bằng `bash`, `node` hoặc `python3`, trong thư mục project (project toàn máy thì chạy ở thư mục home).
  - Script nhận payload qua stdin. Các biến môi trường `OFFICE_PAYLOAD`, `OFFICE_TRIGGER`, `OFFICE_JOB_ID`, `OFFICE_AUTOMATION` cũng được truyền vào.
  - Script chạy trong một process group riêng. Quá timeout thì cả group bị kill.
  - Job loại `script` lưu `output` (64KB cuối của stdout và stderr) và `exit_code`.
  - Exit 0 thì job `done`. Khác 0 thì `failed` với `error_code=script_error`, hoặc `timeout` nếu quá giờ.
- **Gọi AI khi cần** (`escalate {when, action, agent_id, prompt}`):
  - `when` nhận một trong:
    - `never`;
    - `failure` (mặc định): exit khác 0 hoặc quá giờ;
    - `signal`: script in dòng `@@agent: <nội dung>`, cả khi thành công.
  - Khi điều kiện xảy ra, office tạo **job con** (`chat_turn` hoặc `task`) với `parent_job_id` trỏ về job script, `trigger=escalate`, và payload `{output, exit_code, messages, payload}`.
  - Prompt của job con điền thêm được `{{output}}`, `{{exit_code}}`, `{{message}}`. Nội dung đó luôn được đánh dấu là dữ liệu.
- **Tạo bằng trò chuyện:**
  - Agent (từ gói `propose`) có công cụ `propose_automation` (tạo mới, hoặc sửa khi có `automation_id`). Công cụ này tạo thao tác `create_automation` hoặc `update_automation` chờ duyệt. Đặc tả nằm trong `actions.args.automation`.
  - Các thao tác này **luôn cần người duyệt**, vì code sẽ chạy không người xem.
  - Duyệt thì office tạo hoặc sửa tự động hóa. Webhook tạo theo cách này không hiện secret trong Chat: admin lấy secret bằng nút "Đổi secret" ở trang tự động hóa.
- **Dữ liệu** (migration 00021): dựng lại `jobs` để `kind` nhận thêm `script`, và thêm `output`, `exit_code`, `parent_job_id`. Dựng lại `automations` để `action` nhận thêm `script`, và thêm `script` và `escalate` (JSON).
- **Giao diện:**
  - Trình sửa có hành động "Chạy code": chọn ngôn ngữ, ô soạn script, timeout, "Khi nào gọi AI" (kèm agent và prompt).
  - Trang Job có loại Code. Mở job Code thì xem được output và exit code, kèm link sang job con.
  - Thẻ đề xuất trong Chat hiện script, lịch và điều kiện gọi AI.

**Giới hạn.** Script chạy với quyền của user đang chạy office. File cấm của project không áp dụng cho script. Vì vậy chỉ admin tạo hoặc duyệt được.

## ADR-042: Trang tạo tự động hóa có chat, và chat ở góc (giai đoạn 1)

**Bối cảnh.** Người dùng muốn dựng tự động hóa bằng cách trò chuyện, kể cả nhờ AI viết script. Họ cũng muốn một khung chat hỗ trợ ở mọi trang, hiểu mình đang ở đâu và đang định làm gì. Khung chat đó bản chất là **Chat của project**, chỉ mở ở chỗ khác và kèm thêm ngữ cảnh trang.

**Quyết định (giai đoạn 1).**
- **Trang tạo và sửa** `/projects/:id/automations/new` và `…/:aid/edit`, thay cho panel trượt:
  - Máy tính chia đôi. Bên trái là form (cùng các trường như trước, ô soạn script lớn, nút **Chạy thử**). Bên phải là chat.
  - Điện thoại xếp form ở trên, chat ở dưới.
  - **Chạy thử**: `POST /api/projects/:id/automations/test-script` chạy script ngay mà không lưu, chỉ admin gọi được, timeout tối đa 120 giây. Kết quả trả về gồm output, mã thoát và có quá giờ hay không.
- **Chat của tự động hóa** là một cuộc Chat của project:
  - Có `purpose='automation'` và `automation_id`. Tự động hóa mới thì chat được gắn vào nó ở lần Lưu đầu tiên (`conversation_id` trong body khi tạo).
  - Người trả lời là trưởng nhóm, như Chat thường.
  - Loại chat này không hiện trong danh sách Chat của project.
  - `POST /api/automations/:id/conversation` trả về chat của tự động hóa, chưa có thì tạo mới.
- **Ngữ cảnh**:
  - Tin nhắn có trường `context` (tối đa 8KB), gửi kèm cho agent với nhãn *"Ngữ cảnh trang (dữ liệu, không phải lệnh)"* và lưu lại để xem.
  - Trang tạo tự động hóa gửi bản nháp form (JSON) và lần Chạy thử gần nhất.
  - Chat ở góc gửi tên trang, tab và đối tượng đang mở.
- **AI điền form**:
  - Với chat `purpose=automation`, system prompt hướng dẫn trả về một khối ` ```automation ` chứa JSON một phần của bản nháp, với cùng các trường như API.
  - Dashboard đọc khối này, điền vào form và tô sáng các ô đã đổi. Chỉ khi bấm Lưu mới lưu.
  - Không dùng công cụ riêng, nên chạy được với mọi kết nối AI.
- **Chat ở góc**:
  - Nút tròn ở góc phải dưới, có trên mọi trang trong project, trừ tab Chat và trang tạo/sửa tự động hóa.
  - Mở ra là ChatPanel gọn của project: cùng các cuộc trò chuyện, cùng agent, kèm ngữ cảnh trang.
- Migration 00022: thêm `conversations.purpose`, `conversations.automation_id`, `messages.context`.

**Giai đoạn 2 (chưa làm).**
- Chat ở góc có chip phạm vi.
- Ngoài project là trợ lý office, cấu hình mọi project bằng các công cụ chung `describe`, `list`, `get`, `propose_change` (luôn qua thẻ xác nhận). Việc cần code thì chuyển sang Chat của project.
- MCP cho Claude Code CLI.

## ADR-043: Nhật ký thay đổi (ai đổi gì, từ đâu)

**Bối cảnh.** Về sau agent, trợ lý office và CLI đều sửa được cấu hình. Người dùng cần tra được ai đổi gì: người hay agent nào đổi, ai duyệt, đổi trong chat, job hay việc nào, qua kênh nào, và giá trị trước/sau. Trước đây `audit_log` chỉ có `actor` (luôn là người bấm), `action`, `target`, `detail`.

**Quyết định.**
- **Bảng.** Migration 00024 thêm vào `audit_log` các cột:
  - người làm: `actor_kind` (`human`, `agent`, `automation`, `system`), `actor_id`, `actor_name`, `approved_by`;
  - kênh: `via` (`ui`, `chat`, `task`, `assistant`, `mcp`, `automation`, `api`);
  - nguồn: `project_id`, `conversation_id`, `job_id`, `task_id`, `action_id`;
  - đối tượng và giá trị: `resource`, `resource_id`, `before_json`, `after_json`, `ok`.
  - Dòng cũ được suy ra từ `actor`/`action`. Nhật ký chỉ thêm, không sửa, không xóa.
- **Một cổng ghi `internal/audit`.**
  - `audit.With(ctx, Who)` gắn người thực hiện vào context. Không có thì đọc chuỗi `actor` cũ (`human:`, `user:`, `auto:`).
  - `audit.Record` dựng dòng nhật ký. Ghi lỗi thì log, không nuốt.
  - `Snapshot` che bí mật: tên như `api_key`, `token`, `secret`, `password`, `*_hash` ghi thành `***`. Cờ có/không (`has_api_key`) và chuỗi rỗng giữ nguyên.
  - API (`auditAction`, `s.audit`) và gói auth đều ghi qua đây.
- **Trước/sau.** Handler tạo, sửa, xóa ghi bản trước/sau (dạng DTO) cho: tự động hóa, giám sát, tiến trình, agent (kể cả khôi phục), project, quyền & lệnh, kết nối AI, mô hình tổ chức, ngân sách.
- **Thay đổi do agent đề xuất.**
  - Duyệt thẻ thì ghi agent là người làm, người bấm là `approved_by`, kèm chat, việc, job và id thẻ.
  - `actions.job_id` lưu lượt chạy đã đề xuất. `Scope.JobID` do engine truyền vào.
  - Thẻ được duyệt nhưng chạy lỗi ghi `ok=0`. Từ chối là quyết định của người.
  - Patch: duyệt thì ghi agent của cuộc chat, kèm người duyệt. Patch agent tự áp dụng (theo quyền) thì ghi agent, không có người duyệt.
- **Xem.**
  - `GET /api/audit` có lọc (project, resource, resource_id, actor_kind, actor, via, conversation_id, job_id, task_id, since) và phân trang bằng `before` (id).
  - `GET /api/audit/stats?by=day|kind|actor|resource|via|project`.
  - Cả hai chỉ admin gọi được.
  - Dashboard: trang Nhật ký (số liệu 7 ngày cùng bảng có lọc; mở một dòng ra xem diff trước/sau và liên kết tới chat, job, việc), tab Nhật ký trong project, "Lịch sử thay đổi" ở trang chi tiết tự động hóa.

**Để sau.** Ghi thay đổi và nhật ký trong cùng một transaction sẽ làm ở registry cấu hình (spec trợ lý office, phần 2), vì khi đó mọi thay đổi đi qua một đường `Apply` duy nhất.

## ADR-044: Chat tag agent vào nhóm, agent giao việc chạy nền

**Bối cảnh.** Người dùng muốn Chat vẫn là 1-1, nhưng tag `@Agent` thì kéo agent đó vào cuộc chat. Họ cũng muốn agent giao việc cho agent khác giống subagent của Claude Code: người dùng vẫn nói chuyện tiếp, việc giao xong thì người giao báo lại.

**Quyết định.**
- **Thành viên.** Bảng `conversation_agents` (migration 00028) lưu mỗi agent của một cuộc chat, kèm phiên Claude Code riêng, tin cuối cùng agent đó đã thấy, và context của nó.
  - Đến lượt mình, agent nối tiếp đúng phiên của nó (`--resume`) và chỉ nhận các tin mới kể từ lần trả lời trước, có ghi tên người nói.
  - Agent vào cuộc chat lần đầu thì nhận transcript.
  - Đổi agent ở ô nhập chỉ đổi người trả lời mặc định, không mất phiên của ai.
- **Tag.** `Mentions` nhận `@Tên` hoặc `@key`: không phân biệt hoa thường, ưu tiên tên dài nhất, bỏ qua tag nằm trong code và tag dính giữa một từ.
  - Tin không tag ai: agent đang chọn ở ô nhập trả lời.
  - Người dùng tag nhiều agent: các agent trả lời lần lượt. Tối đa 4 agent cho một tin. Sự kiện `done` mang `next_turn_id` để dashboard theo tiếp lượt sau.
- **Agent giao việc cho agent.**
  - Agent tag agent khác thì agent kia chạy **ở nền**. Người dùng không bị chặn, vẫn nhắn tiếp được.
  - Agent ở nền làm xong thì agent đã giao việc tự báo lại, nếu lúc đó cuộc chat đang rảnh. Nếu đang bận, agent giao việc sẽ thấy kết quả ở lượt kế tiếp của nó.
  - Mỗi tin của người dùng cho phép tối đa 2 lượt chuyển. Quá mức thì dừng và ghi một dòng ghi chú.
  - Tag một agent đang làm ở nền thì bị báo bận (`ErrAgentBusy`).
  - Mỗi agent vào cuộc chat sau có worktree riêng (`chat-<id>--<agent>`), để các agent chạy cùng lúc không đè lên nhau.
  - Prompt dặn agent: chỉ tag khi thật sự cần.
- **API.** `GET /api/conversations/:id` trả thêm `members` và `running` (lượt nào đang chạy, có phải chạy nền không).
- **Dashboard.**
  - Gõ `@` hiện danh sách agent để tag.
  - Hiện hàng thành viên của cuộc chat.
  - Nhãn "X đang làm…" cho agent đang chạy nền, có nút dừng.
  - Tag trong tin của người dùng được tô màu.
- **Lead mặc định có quyền Vận hành:** `team-lead`, `assistant`, `executor`, áp cho cả template và các agent chưa tự chỉnh quyền. Instructions của team-lead đổi thành: trong Chat thì tự làm, trong Việc thì chia việc.

## ADR-045: Registry cấu hình và bốn công cụ chung (trợ lý office, phần 2)

**Bối cảnh.** Agent (và sắp tới là trợ lý office, CLI) cần xem và đổi được mọi cài đặt, qua thẻ duyệt, không phải viết riêng từng công cụ.

**Quyết định.**
- **Registry** (`internal/api/config_registry.go`). Mỗi loại cài đặt khai báo: cách lấy (đã che bí mật), cách liệt kê, và **handler API của dashboard** cho tạo, sửa, xóa. Các loại hiện có: `automation`, `agent`, `monitor`, `process`, `policy`, `project`, `usage_settings`, `provider`.
- **Công cụ:** `describe` (loại cài đặt và các trường), `list`, `get`, `propose_change(resource, op, id, patch, reason)`.
- **Lúc đề xuất:**
  - Kiểm tra: loại cài đặt có tồn tại, thao tác hợp lệ, trường sửa được, id có thật và thuộc đúng project.
  - Lưu bản hiện tại (`before`).
  - Tạo action `config_change`. Loại này luôn chờ người duyệt.
- **Lúc duyệt:**
  - Đọc lại bản hiện tại. Nếu khác `before` thì từ chối và báo: "đã có thay đổi mới, hãy đề xuất lại".
  - Ghép patch lên bản hiện tại (với handler nhận cả object), lọc theo đúng các trường input của handler.
  - **Gọi chính handler đó** với người duyệt trong context. Kiểm tra dữ liệu, ghi và nhật ký vì thế chạy y như khi sửa trên dashboard. Nhật ký ghi agent là người đề xuất, kèm người duyệt.
- **Kết nối AI:**
  - Patch có `api_key` thì bị từ chối.
  - Thẻ tạo kết nối có ô dán key. Key đi từ trình duyệt vào lệnh duyệt (`POST /api/actions/:id/approve {api_key}`), AI không bao giờ thấy.
- **Thẻ duyệt** hiện loại cài đặt, thao tác, và bảng trước/sau của từng trường.
- **Để sau:** `org_model`; cho `actions.project_id` được rỗng (cần khi có trợ lý ngoài project).
- **Sửa sau review:**
  - Đề xuất chỉ bị coi là cũ khi **các trường sửa được** đổi. Lúc đề xuất lưu `hash` (SHA-256 của các trường đó, chưa che bí mật) và `before` (đã che, để hiện trên thẻ). Một lần chạy làm đổi `last_run_at` hay số lần lỗi không làm đề xuất bị cũ. Còn bí mật bị đổi thì vẫn bị phát hiện.
  - `propose_change` cần quyền Đề xuất trở lên, trừ phạm vi office.
  - `usage_settings` và `provider` là cài đặt chung: chỉ đề xuất được từ phạm vi office (trợ lý office hoặc CLI).

## ADR-046: Trợ lý office (phần 3)

**Bối cảnh.** Người dùng cần một trợ lý của cả office: biết việc thuộc project nào, làm thống kê và báo cáo, cài đặt, điều phối.

**Quyết định.**
- **Project hệ thống "Office".** Office tự dựng khi khởi động (`internal/assistant`, id lưu trong settings). Thư mục làm việc là `.office/assistant/`, để trống.
  - Chỉ có một agent: "Trợ lý office", quyền Chỉ đọc, model Cân bằng, kèm instructions riêng.
  - Project này ẩn khỏi danh sách project.
  - Chat của trợ lý dùng lại nguyên bộ chat hiện có: phiên, job, chi phí, thẻ duyệt.
  - Mỗi người chỉ thấy các cuộc chat trợ lý của chính mình.
- **Phạm vi office** (`Scope.Office`). Chat của trợ lý chạy ở phạm vi này:
  - Công cụ của project nhận thêm `project` (id hoặc tên).
  - Công cụ riêng của trợ lý:
    - `projects`, `jobs_query`, `usage_summary`: đọc số liệu.
    - `handoff`: trả liên kết `/projects/:id?tab=chat&draft=…`. Chat của project mở một cuộc chat mới, có sẵn tin nhắn cho người dùng gửi. Trợ lý không sửa code.
    - `start_task`, `run_automation`: tạo thẻ duyệt. Duyệt xong mới giao Việc (cho cả đội hoặc một agent) hay chạy tự động hóa.
  - `propose_change` ở phạm vi office nhắm vào project được chỉ định.
- **Giao diện:**
  - Mục "Trợ lý office" trên sidebar, trang `/assistant`.
  - Chat ở góc: ngoài project là trợ lý. Trong project có nút chọn "Project này" hoặc "Toàn office".
- **Sửa sau review:** chat trợ lý là riêng của từng người, và điều này được kiểm ở phía server:
  - mở, gửi tin, xem stream, dừng, xóa cuộc chat của người khác đều trả 404;
  - job của những cuộc chat đó không hiện ở trang Jobs;
  - `jobs_query` bỏ qua job của project Office;
  - `read_link` không đọc được chat của trợ lý.

## ADR-047: MCP cho Claude Code CLI của người dùng (phần 4)

**Quyết định.**
- **Token cá nhân** (bảng `user_tokens`, migration 00033). Token có dạng `ofc_…`, chỉ lưu SHA-256.
  - Tạo ở trang Tài khoản. Token chỉ hiện một lần, kèm lệnh `claude mcp add --transport http agent-office http://<máy>:8787/mcp --header "Authorization: Bearer …"`.
  - Có thể thu hồi. Hệ thống ghi lại lần dùng cuối.
- **Xác thực ở `/mcp`.** Bên cạnh token theo từng lượt chạy, `/mcp` nhận token cá nhân và đổi thành **phạm vi office** của người đó.
  - Agent hiện là "Claude Code CLI (email)", quyền Đề xuất, không chạy gì trên máy.
  - Dùng được mọi công cụ của trợ lý (ADR-046). Mọi thay đổi vẫn qua thẻ duyệt.
- **Hộp "Chờ duyệt"** (`GET /api/actions/pending`, chỉ admin) nằm trên trang Trợ lý. Đề xuất từ CLI không thuộc cuộc chat nào nên được duyệt ở đây.
- Công cụ kiểm tra tên theo phạm vi (`Has(scope, name)`), để các công cụ riêng của phạm vi office gọi được qua MCP.
- **Để sau:** chế độ "áp dụng ngay" cho token của admin. Hiện mọi đề xuất đều qua thẻ.

## ADR-048: Kênh Telegram / Discord hai chiều

**Quyết định.**
- **Kênh** (bảng `channels` và `channel_threads`, migration 00034) thuộc một project. Mỗi kênh có: loại, tên, token (mã hóa bằng secrets box, chỉ ghi không đọc lại), agent trả lời (mặc định là trưởng nhóm), quyền tối đa (mặc định Chỉ đọc), danh sách chat/user id được phép, phạm vi trả lời, bật/tắt bộ lọc, câu từ chối. Mỗi cuộc chat bên ngoài ứng với một conversation.
- **Telegram:** dùng long polling (`getUpdates`), không cần URL công khai. Chat riêng thì luôn trả lời. Trong nhóm chỉ trả lời khi bot được tag hoặc được reply.
- **Discord:** kết nối Gateway qua một websocket client tối giản tự viết (`internal/channels/ws.go`), không thêm dependency. Dùng các intents guild messages, direct messages, message content. Trả lời tin nhắn riêng hoặc khi bot được tag. Gửi tin qua REST.
- **Mỗi tin đến:**
  1. Chat không nằm trong danh sách được phép thì im lặng.
  2. Nếu bật lọc: `Engine.Invoke` với model *nhanh* hỏi YES/NO về phạm vi. Câu ngoài phạm vi nhận câu từ chối, và được ghi một job `skipped` mã `out_of_scope` để thống kê.
  3. Câu trong phạm vi: tạo job (origin `user`, trigger `telegram`/`discord`, `origin_id` là id kênh), gửi vào conversation với mode = quyền tối đa của kênh. Trong lúc chờ gửi trạng thái "đang gõ", xong thì gửi câu trả lời (cắt 4000 ký tự với Telegram, 1900 với Discord).
- **Chạy kênh:** `channels.Manager` chạy các kênh đang bật và khởi động lại kênh khi cấu hình đổi. Trạng thái lưu gồm tên bot, lỗi, tin gần nhất.
- **Giao diện:** mục **Kênh chat** trong project (chỉ admin), có form tạo/sửa và hướng dẫn tạo bot.
- **Ruling:** cột `origin` của jobs không có giá trị `channel`. Để không phải dựng lại bảng jobs, lượt từ kênh được ghi là `origin=user`, còn kênh nào thì phân biệt qua `trigger` và `origin_id`.
- **Sửa sau review:**
  - Lượt từ kênh chạy **không có công cụ**: không MCP, không đọc hay sửa file. Bộ lọc phạm vi cũng vậy.
  - Danh sách được phép là bắt buộc: `*` là ai cũng nhắn được, còn để trống thì không ai nhắn được. Muốn bật kênh cần ít nhất một dòng.
  - Mỗi cuộc chat chỉ có tối đa 3 tin chờ; tin dồn dập vượt quá bị bỏ.
  - Tag Unicode không làm crash nữa, và panic không làm sập office.
  - Lỗi không để lộ token.
  - Telegram thử lại `getMe` khi lỗi mạng, chỉ dừng khi token sai.
  - Discord dừng và báo lý do với các mã đóng không tự khỏi được (4004, 4010–4014; 4014 là chưa bật Message Content Intent). Sau một phiên chạy tốt, thời gian chờ nối lại quay về 1 giây.

## ADR-049: Tin nhắn kênh là nguồn của tự động hóa

**Bối cảnh.** Theo ADR-048, mỗi kênh có đúng một cách trả lời: một agent trả lời, kèm bộ lọc phạm vi. Nhưng tin từ kênh cũng là một sự kiện như webhook. Người dùng cần một bot làm được nhiều việc: tra dữ liệu bằng script (không tốn token), giao Việc cho đội, hoặc để agent trả lời.

**Quyết định.**
- **Kênh chỉ còn là kết nối:** token, trạng thái, danh sách được phép, và câu trả lời khi không quy tắc nào khớp (trống thì im lặng). Các cột agent, mode, scope, filter cũ vẫn để trong bảng nhưng không dùng nữa.
- **Quy tắc** là một tự động hóa có nguồn `telegram` hoặc `discord`. Config gồm `channel_id` (kênh cùng project, đúng loại), `keywords` (tin chứa một trong các từ, không phân biệt hoa thường; trống = mọi tin) và `scope` (có thì model nhanh hỏi YES/NO).
- **Chọn quy tắc:** xét các quy tắc đang bật của kênh theo thứ tự tạo, quy tắc đầu tiên khớp sẽ nhận tin. Từ khóa được xét trước vì không tốn gì; phạm vi AI chỉ được hỏi khi từ khóa đã khớp. Không quy tắc nào khớp thì bot gửi câu trả lời mặc định và ghi một job `skipped` mã `no_rule`.
- **Chạy:** `trigger.Runner.Enqueue`, dùng chung số lượt mỗi giờ, trần chi phí, tự tắt khi lỗi liên tục và escalate của tự động hóa. Payload là `{message, user, user_id, chat_id, channel_id, conversation_id}`.
- **Hành động:**
  - `chat` ("Trả lời trong chat"): **mặc định mỗi tin là một conversation mới**, agent không nhớ tin trước. Người nhắn gửi `/create-conversation` để bắt đầu một hội thoại được giữ lại: các tin sau nối tiếp, ngữ cảnh gắn theo (chat bên ngoài, agent), nên đổi quy tắc mà cùng agent thì vẫn nhớ. Gửi `/close-conversation` để quay về mặc định. Lệnh nhận cả `_` thay cho `-`, chữ `conversion`, và dạng `/lệnh@bot` của Telegram. Lệnh được đăng ký vào menu: Discord đăng ký slash command (`PUT /applications/:id/commands`) mỗi khi bot kết nối. Slash command đi xuống qua gateway (không cần URL công khai): bot xác nhận ngay (type 5, Discord hiện "đang suy nghĩ…") rồi sửa lại tin đó khi có câu trả lời. Telegram dùng `setMyCommands` với dạng gạch dưới. Gõ lệnh không cần tag bot, kể cả trong nhóm và server. Adapter đẩy lên mọi tin, kèm cờ `Addressed` (tin nhắn riêng, có tag bot, reply vào tin của bot, hoặc là lệnh). Manager chỉ xử lý tin `Addressed`, trừ khi cuộc chat đó đang giữ hội thoại: khi ấy bot nghe mọi tin trong kênh hay nhóm (của người được phép) mà không cần tag, cho tới `/close-conversation`.
  - **Reply vào một câu trả lời của bot** thì tiếp tục đúng conversation đã sinh ra câu trả lời đó, kể cả khi không giữ hội thoại. Reply cũng quay về **đúng tự động hóa** đã trả lời, không chọn lại quy tắc từ đầu (mapping `channel_rule/<kênh>/<tin>` lưu ở settings). Câu trả lời của slash command, tức là tin được sửa từ "đang suy nghĩ…", cũng được ghi nhớ id qua kết quả của `PATCH @original`. `Adapter.Send` trả về id của các tin đã gửi; mỗi id được lưu thành thread `msg:<id>`, trỏ tới conversation. Tin đến mang `ReplyTo`. Reply vào câu trả lời mới cũng nối tiếp được, nên chuỗi reply không bị đứt.
  - **`/job <việc>`** (slash command có tham số `viec`, và Telegram): giao Việc qua tự động hóa của bot. Ưu tiên tự động hóa đầu tiên đang bật có hành động task; nếu không có thì dùng tự động hóa đầu tiên của bot, nhưng chạy dưới dạng task (payload `action: "task"`). Vì vậy việc này dùng chung agent, giới hạn và chi phí của tự động hóa đó, và vẫn phải qua danh sách người được nhắn. Bot báo "Đã nhận việc…" ngay, rồi gửi kết quả khi Việc xong.
  - Việc và cuộc chat sinh ra từ bot ghi người tạo là `<kênh>:<người nhắn>` (trước đây ghi `auto:<tự động hóa>`).
- **Chỉ dẫn nằm trong system prompt:** với hành động Trả lời, Nội dung gửi của tự động hóa là chỉ dẫn của người quản trị. Chỉ dẫn này đi vào **system prompt** của lượt chạy (`trigger.WithInstructions`, rồi `chat.WithInstructions`), còn tin nhắn gửi cho agent chỉ là thứ người dùng gửi. Nếu để chỉ dẫn trong tin nhắn thì model coi đó là prompt injection và từ chối, hoặc chỉ "xác nhận" chứ không làm theo. Lệnh không kèm nội dung thì gửi "X gọi lệnh /cmd.", không gửi "/cmd", vì chat hiểu dấu `/` ở đầu là gọi skill. Hành động Giao Việc vẫn ghép chỉ dẫn và tin nhắn thành mục tiêu của Việc.
- **Lệnh từ skill:** trong trang setup bot, nút "Thêm từ skill" liệt kê các skill của project (của project, của máy và của plugin, như `/api/projects/:id/skills`). Có ô tick từng skill và "Chọn tất cả"; skill đã thêm thì hiện "đã có".
  - Mỗi skill thành một lệnh: tên suy từ tên skill (`plugin:skill` thành `plugin-skill`, thêm hậu tố nếu trùng), mô tả lấy từ skill, hành động Trả lời, `config.skill` là tên skill.
  - Khi chạy, chat nhận `/skill <nội dung>` và engine nạp chỉ dẫn của skill (`ExpandSkillCall`). Lượt từ bot vẫn không có công cụ.
  - Mô tả lệnh được cắt còn 100 ký tự cho Discord, 256 ký tự cho Telegram.
- **Lệnh custom** là một tự động hóa của bot, có điều kiện "Lệnh" thay cho từ khóa:
  - Config gồm `command`, `command_description`, `command_arg`. Tên lệnh được chuẩn hóa: chữ thường, không dấu, nối bằng `-`, tối đa 32 ký tự. Tên không được trùng lệnh có sẵn (`job`, `create-conversation`, `close-conversation`, `start`, `help`) hay lệnh khác của cùng bot.
  - Lệnh chỉ chạy khi được gọi đúng tên; `pick` và `/job` bỏ qua các tự động hóa có lệnh. Phần chữ sau lệnh là `{{message}}`; lệnh cần nội dung mà không có thì bot nhắc cách gõ.
  - Mỗi lần kết nối, `Adapter.SetCommands` đặt menu gồm lệnh có sẵn và lệnh custom (Discord `PUT commands`, Telegram `setMyCommands`). Lưu một tự động hóa là khởi động lại bot, nên menu luôn mới. Gõ lệnh trong menu không cần tag bot.
  - Slash command custom có hành động chat hoặc script thì câu trả lời sửa lại chính tin "đang suy nghĩ…". Câu trả lời dài hơn 1900 ký tự, hoặc sau khi token đã hết hạn, thì gửi thành tin mới.
  - Form có danh sách "Lệnh của bot": lệnh có sẵn chỉ để xem, lệnh custom thì link tới tự động hóa của nó. Danh sách này thay cho đoạn giải thích cũ.
  - **Trang setup bot** (`/projects/:id/bots/:channel`, hoặc `new`): nhìn từ phía bot. Bên dưới, mỗi câu lệnh vẫn là một tự động hóa.
    - **① Bot:** chọn Discord/Telegram (khi tạo mới) hoặc xem trạng thái; cài đặt chung thu gọn; nút ⓘ mở hướng dẫn.
    - **② Câu lệnh:** mỗi dòng bấm để mở ra phần hành động, agent, nội dung gửi/script và giới hạn của riêng nó. Phần này dùng lại `AutomationForm` ở chế độ `command`.
      - `@bot <tin nhắn>` là lệnh cơ bản, luôn có, có thể chỉ nhận một số tin (từ khóa, chủ đề).
      - Lệnh `/` custom thêm, xóa được.
      - Các lệnh hệ thống (`/job`, `/create-conversation`, `/close-conversation`) có khóa.
    - **Lưu:** lệnh đầu tiên mang theo bot (tạo hoặc sửa bot); các lệnh sau trỏ tới bot đó; lệnh bị xóa được xóa sau cùng.
    - AI bên cạnh điền vào câu lệnh đang mở.
  - **Trang chi tiết bot** (`/bots/:id`): trạng thái kèm công tắc bật/tắt cả bot; các câu lệnh với hành động, trạng thái bật và lần chạy gần nhất (link tới chi tiết của lệnh); lịch sử job của cả bot. Lịch sử này lọc `origin_ids` gồm các lệnh và chính bot, để thấy cả tin không lệnh nào nhận. Nút Sửa mở `/bots/:id/edit`; tạo mới ở `/bots/new`; lưu xong thì về trang chi tiết.
  - Danh sách Tự động gom mỗi bot thành **một dòng** (tên bot, số lệnh, các lệnh `/`, lỗi, lần chạy gần nhất). Menu gồm Mở setup, Kết nối lại, Xóa bot (xóa mọi lệnh). Form tự động hóa chọn "Tin nhắn kênh" thì chuyển sang trang bot mới; trang sửa của một lệnh bot chuyển về trang bot.
- **Nguồn:** `actor.Source(created_by)` trả về web, discord, telegram hoặc auto. DTO của cuộc chat và của Việc có trường `source`; danh sách chat (`?source=`, `all` gồm cả chat của bot) và danh sách Việc lọc được theo nguồn. Giao diện có ô lọc ở đầu hai danh sách, và biểu tượng nguồn trên từng dòng. Người không được phép mà dùng slash command thì nhận câu "chưa được phép", vì Discord luôn chờ một câu trả lời. Trạng thái lưu ở settings `channel_keep/<kênh>/<chat>` dưới dạng một mã thế hệ: mỗi lần create là hội thoại mới hẳn. Trong nhóm, cả nhóm dùng chung một hội thoại. Lượt này luôn **không có công cụ** và chỉ đọc. Prompt mặc định là `{{message}}`, tức chính câu hỏi; có thêm `{{user}}`.
  - `task`: gửi ngay "Đã nhận, đội đang xử lý". Xong Việc thì gửi `Result` (không có thì gửi `Detail`).
  - `script`: stdout (đã bỏ các dòng `@@agent:`) là câu trả lời. Nếu script gọi agent thì câu trả lời của agent được gửi tiếp sau.
- **Gửi lại:** `Runner.SetOnReply(func(ctx, origin, reply, err, final))`. `origin` là job mang payload của kênh; với agent do script gọi thì là job của script. `Executor.RunChat` trả thêm câu trả lời cuối. Lỗi chỉ được báo cho người ngoài bằng một câu chung; chi tiết nằm ở job. Mỗi chat vẫn giới hạn 3 tin chờ, nhả ra khi có câu trả lời cuối. Trạng thái "đang gõ" được gửi đều đặn tới khi xong.
- **Kênh cũ:** khi khởi động, mỗi kênh có sẵn được tạo một quy tắc "Trả lời" từ agent và phạm vi cũ. Việc này chỉ làm một lần (settings `channels_rules_v1`). Ngữ cảnh của các cuộc chat cũ không được mang sang.
- **Bot là cấu hình của trigger, không phải một thứ riêng.** Lịch chạy, webhook hay bot đều là trigger, nên chỉ có một danh sách và một form (có AI hỗ trợ).
  - API tự động hóa nhận `bot {token, allow, refusal}`. Chưa có `channel_id` thì tạo bot mới (cần token); có rồi thì sửa bot đó. Tự động hóa bị từ chối thì bot vừa tạo bị xóa theo.
  - DTO có `bot` (các trường sửa được: `has_token`, `allow`, `refusal`) và `bot_status` (tên bot, lỗi, tin gần nhất, số tự động hóa dùng chung). `bot_status` tách riêng để một tin mới không làm đề xuất bị cũ.
  - Nhiều tự động hóa dùng chung một bot. Xóa tự động hóa cuối cùng dùng bot thì bot cũng bị xóa và dừng.
  - Token không bao giờ đi qua chat: `mergeDraft` bỏ qua `bot.token`, ngữ cảnh gửi AI bỏ token, còn `propose_change` từ chối `bot.token`.
  - Form: bước Nguồn có "Tin nhắn kênh", gồm chọn bot hoặc "+ Bot mới" (Telegram/Discord, các bước hướng dẫn đánh số, token, người được nhắn, câu trả lời mặc định), rồi từ khóa và chủ đề. Cài đặt của bot có sẵn được thu gọn, kèm cảnh báo là dùng chung.
  - Danh sách tự động hóa hiện `@tên_bot · Discord · "từ khóa"`, và báo lỗi bot ngay trên dòng. Không còn tab hay dải bot riêng; link cũ `?tab=channels` chuyển về tab Tự động.
  - AI dựng tự động hóa biết nguồn telegram/discord, `action=chat` (chỉ dùng cho bot) và thứ tự quy tắc.
- **Để sau:** agent đề xuất tự động hóa (`trigger.Spec`) vẫn chỉ tạo được lịch chạy và webhook.

## ADR-050: Soạn skill trong office, có AI

**Quyết định.**
- Tab Skill của project có nút **Tạo skill**, và menu của mỗi skill (của project hoặc của máy, loại sửa được) có mục **Sửa**. Cả hai mở `/projects/:id/skills/edit` (`?name=&scope=project|user`).
- **Trình soạn:** gồm tên (cũng là lệnh `/tên`; khi sửa thì không đổi được, vì tên là tên thư mục), mô tả (skill làm gì và khi nào dùng) và nội dung SKILL.md (Markdown). Các dòng frontmatter khác và các file khác của skill được giữ nguyên.
- **Lưu:** gọi `POST /api/automation/install` với `files` gửi thẳng lên (trường mới của `InstallRequest`), qua cùng bước kiểm tra an toàn như khi cài từ thư viện: có cảnh báo thì hỏi lại, có nội dung bị từ chối thì báo lỗi. Sửa là ghi đè; bản cũ được đưa vào thùng rác.
- **AI:** loại chat `skill` (không nằm trong danh sách chat của project) dùng hướng dẫn `skillGuide`. AI trả về khối ```` ```skill {name, description, body} ```` và trình soạn lấy nội dung đó điền vào, nhưng không đổi tên khi đang sửa. AI không tự ghi file; người dùng xem lại rồi bấm Lưu.
- Bỏ thông báo "Đây là mã nguồn của chính office" ở đầu project: đây là thông báo cố định, không báo có cập nhật, nên chỉ gây nhiễu.

### ADR-049/050: sửa sau review (29/09)
- **C1:** mapping reply (`msg:` và `channel_rule/`) gắn theo từng chat, vì Telegram đánh số tin riêng cho mỗi chat. Nếu không, reply vào tin của mình có thể nối vào hội thoại của người khác.
- **C2:** agent do script của bot gọi vào (escalate) chạy ở chế độ không tin cậy: không công cụ, chỉ đọc, conversation `purpose=channel`, và luôn là chat (không bao giờ là Việc).
- **I1:** trong hội thoại của bot, `/tên` chỉ nạp skill mà lệnh đó đã chọn (`WithSkill`); các `/…` khác được gửi như tin thường. Mục tiêu Việc sinh từ tin của bot không bao giờ bắt đầu bằng `/`.
- **I2:** `/job` chỉ chạy qua một tự động hóa có hành động Giao Việc của bot; bot không có tự động hóa như vậy thì trả lời "chưa bật giao việc".
- **I3:** chỉ dẫn trong system prompt không bao giờ chứa lời của người dùng: `{{message}}` và `{{user}}` được thay bằng nhãn, còn lời người dùng chỉ nằm trong tin nhắn.
- **I4:** khi script gọi agent, job con được tạo trước; tạo không được thì câu trả lời của script là câu cuối, nên chat không bị treo.
- **I5:** thay đổi của bot có sẵn chỉ được ghi sau khi tự động hóa đã được lưu.
- **I6:** đọc và ghi `SKILL.md` qua `utils/skillMd.ts`: đọc được mô tả kiểu `|` hoặc `>`, file CRLF, chuỗi có nháy; các khóa khác giữ nguyên thứ tự; ghi lại nhiều lần vẫn cho cùng một kết quả.
- **Lỗi nhỏ:** lệnh slash không được nhận vẫn có câu trả lời, thay vì treo "đang suy nghĩ…"; menu tối đa 100 lệnh, lệnh có sẵn đứng đầu; bot đã dừng thì không gửi tin nữa.
- **Để sau:** dọn các dòng thread và keep tồn đọng; kiểm tra `/cmd@botkhác` trong nhóm Telegram; lưu bot bị lỗi giữa chừng ở BotSetup (bot mới và lệnh thứ hai lỗi); audit `channel.create` không có dòng xóa tương ứng.

## ADR-051: Dashboard — Job, Sự cố, sidebar

**Quyết định.**
- **Job:**
  - Mỗi lần khởi động, các Việc có từ trước khi có job được bù job (`tasks.BackfillJobs`), mang đúng ngày giờ, trạng thái và chi phí của Việc. Nhờ vậy trang Job thấy đủ lịch sử. Trước đây trang Job chỉ thấy từ 28/09, nên nhìn như bộ lọc bị sai.
  - Bộ lọc mới: tìm theo tiêu đề (`q`) và nguồn (`source` = web, discord, telegram, auto) thay cho ô "Nguồn" cũ. Cột Nguồn có icon.
- **Sự cố** (`GET /api/incidents`, trang `/incidents`) thay cho trang giữ chỗ Incidents. Trang gom những gì cần người xử lý trên mọi project, trừ project Office: giám sát đang báo lỗi, tiến trình chết, tự động hóa office đã tắt, bot mất kết nối, job lỗi trong 24 giờ, và thẻ chờ duyệt (chỉ admin thấy). Mỗi mục có link tới chỗ xử lý; mục lỗi xếp trước, mới trước cũ.
- **Bỏ Blackboard:** việc các agent trao đổi đã có ở chat nhóm (tag, `delegate`) và trao đổi trong Việc.
- **Sidebar** chia hai nhóm:
  - **Làm việc:** Tổng quan, Trợ lý office, Job, Sự cố (có badge đếm, làm mới mỗi phút).
  - **Cài đặt:** Kết nối AI, Mô hình, Thư viện, Chi phí.
- **Tổng quan:**
  - Thẻ "Cần xử lý" (5 sự cố đầu tiên).
  - Số liệu 24 giờ: đang chạy, lỗi, chi phí, số project.
  - Các bước thiết lập ban đầu chỉ hiện khi còn bước chưa xong.
  - Việc gần đây.
  - Bỏ thẻ giám sát, vì đã nằm trong Sự cố.
- **Sửa (theo yêu cầu):** gộp Sự cố vào **Tổng quan**. Tổng quan là nơi theo dõi chung duy nhất: hiện toàn bộ danh sách cần xử lý (làm mới mỗi 30 giây), badge đếm nằm trên mục Tổng quan, bỏ menu Sự cố, và link `/incidents` chuyển về Tổng quan. Job là nơi xem chi tiết.

### ADR-049: quyền của cuộc chat từ bot (29/09, theo yêu cầu)
- **Quyền đi theo agent:** cuộc chat từ bot chạy bằng **đúng quyền của agent được chọn trả lời** (công cụ, office, MCP theo cài đặt của agent), như trên web, và không còn bị ép "không công cụ". Người quản lý bot kiểm soát ai được dùng qua ô "Ai được nhắn bot". Trang setup bot hiện cảnh báo đỏ khi ô này có `*`.
- Agent do script của bot gọi vào cũng chạy theo quyền của nó; bỏ cơ chế "không tin cậy" ở C2.
- Web gửi tiếp được vào cuộc chat của bot.
- **Vẫn giữ:** lời người dùng không vào system prompt (I3); `/tên` chỉ nạp skill của lệnh (I1); `/job` cần tự động hóa Giao Việc (I2).
- **Lượt chạy không công cụ** (hiện chỉ còn bước lọc chủ đề YES/NO) có thêm `--strict-mcp-config`. Trước đây MCP trong cấu hình cá nhân (Jira…) vẫn được nạp dù đã đặt `--tools ""`.

## ADR-052: Chỉ đạo Việc đang chạy qua chat

**Bối cảnh.** Việc chạy nhiều bước, người dùng chỉ xem được mà không can thiệp; chat với quản lý chỉ mở sau khi Việc xong.

**Quyết định.**
- Cuộc trao đổi của Việc luôn hiện trong trang Việc, kể cả khi đang chạy.
- Tin nhắn người dùng gửi vào đó từ lúc Việc bắt đầu được đưa vào **mọi bước sau** (lập kế hoạch lại, worker, giám sát, tổng hợp), dưới mục "Chỉ đạo của người dùng trong lúc Việc chạy". Chỉ đạo được ưu tiên hơn kế hoạch nếu mâu thuẫn. Mỗi bước nhận toàn bộ chỉ đạo (tối đa 10 tin gần nhất), để các việc chạy song song đều biết.
- Trong lúc Việc chạy, quản lý trả lời ngắn (xác nhận chỉ đạo, báo tiến độ) và **không sửa code**: lượt chat không có quyền ghi. Đội mới là người sửa. Muốn đổi hẳn mục tiêu thì dừng Việc rồi giao lại.

**Hệ quả.** Bước đang chạy dở không nhận chỉ đạo mới, chỉ các bước bắt đầu sau đó.

## ADR-053: Bot giao việc cho đội

**Bối cảnh.** Cuộc chat từ Discord/Telegram không giao được việc cho agent khác ("chỉ giao việc được trong Chat"), nên trưởng nhóm, vốn không tự sửa code, không làm được gì. Các cuộc chat bot tạo trước ADR-049 vẫn giữ trần "Chỉ đọc".

**Quyết định.**
- Chat của bot (`purpose = channel`) là chat của đội: có công cụ `delegate` và phần giới thiệu đội như chat trên web.
- Báo cáo của đội sau câu trả lời được gửi tiếp vào kênh. Đó là các tin của agent trả lời khi được gọi lại, kèm lỗi nếu có. Việc gửi tiếp dừng khi cuộc chat im lặng, khi người dùng nhắn tiếp, hoặc sau 1 giờ.
- Migration 00035 nâng các cuộc chat bot đang ở `read` lên `operate`, để chúng chạy theo quyền của agent.
