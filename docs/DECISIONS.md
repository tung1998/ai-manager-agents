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

