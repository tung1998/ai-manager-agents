# CLI `office`

## Quy ước chung

| Flag toàn cục | Mặc định | Ý nghĩa |
|---|---|---|
| `-c, --config` | `./office.config.json` | Đường dẫn config |
| `--data-dir` | `./.office` | DB SQLite, blobs, history |
| `--json` | tắt | Output máy đọc được |
| `-v, --verbose` | tắt | Log chi tiết ra stderr |
| `--no-color` | tự phát hiện TTY | |

| Exit code | Ý nghĩa |
|---|---|
| 0 | Thành công |
| 1 | Lỗi chung |
| 2 | Sai tham số / config không hợp lệ |
| 3 | Kiểm tra `doctor` có lỗi chặn |
| 4 | Hết budget |
| 5 | Người dùng hủy |

Mọi lệnh có tác động ghi (`init`, `add`, `tune`, `learn`, `approve`) đều hiện preview và hỏi xác nhận, trừ khi có `--yes`.

---

## `office init`

Quét repo, đề xuất cơ cấu, sinh `office.config.json` + `knowledge.md`. Không ghi file khi chưa duyệt. Chạy lại trên repo đã có config thì chỉ hiện diff.

| Flag | Ý nghĩa |
|---|---|
| `--static` | Chỉ quét tĩnh + detect runtime, không gọi LLM (M0) |
| `--budget-usd <n>` | Budget ngày mong muốn, ảnh hưởng chọn model |
| `--max-managers <n>` | Giới hạn trên, 2–5 |
| `--runtime <name>` | Ép runtime ưu tiên |
| `--out <path>` | Ghi đề xuất ra file thay vì ghi config |
| `--yes` | Tự chọn "Tạo" |

```text
$ office init
▸ Quét tĩnh storefront-v5 ............................ 0.8s
  manifest   package.json (nuxt 3, pinia, @stripe/stripe-js, ioredis, winston-graylog2)
  infra      docker-compose.yml, Dockerfile-alpine, bitbucket-pipelines.yml
  env        19 biến (NUXT_REDIS_URL, NUXT_DISCORD_HOOK, …)
  mcp        không có .mcp.json trong repo; phát hiện MCP Hub ở user scope
▸ Runtime    claude-code 2.x ✓   codex ✗   gemini ✗   ANTHROPIC_API_KEY ✗
▸ Phân tích domain (claude-sonnet-5, ~6k token) ...... 11s

Đề xuất cơ cấu                                     ước tính ~$3.10/ngày
  Director        claude-opus-5-5 → fallback sonnet
  Managers
   ✚ tech-lead       sonnet   reason: SSR Nuxt, Redis cache, log Graylog
   ✚ data-analyst    sonnet   reason: storefront có funnel + đơn hàng (Metabase)
   ✚ risk-security   sonnet   reason: Stripe checkout, dữ liệu khách hàng
  Workers
   ✚ code-worker     haiku    nguồn: repo (read-only)
   ✚ graylog-worker  haiku    nguồn: MCP graylog-*
   ✚ metabase-worker haiku    nguồn: MCP metabase-*
  Đề xuất kết nối (chưa có MCP): stripe, redis, tracking

[T]ạo  [C]hỉnh  [H]ủy ? T
✓ Ghi office.config.json, knowledge.md (history: .office/config-history/20260924T101500.json)
```

Chạy lại:

```text
$ office init
Config đã tồn tại. Diff đề xuất:
  managers
  ~ tech-lead.heartbeat.normal.max_minutes   120 → 90   reason: log tăng 3x từ lần init trước
  workers
  + sentry-worker   reason: phát hiện @sentry/nuxt trong package.json
[Á]p dụng  [C]hỉnh  [H]ủy ?
```

## Mô hình, repo, kết nối (đã làm)

`office init` hiện tại: đăng ký repo, chọn mô hình tổ chức, phát hiện kết nối AI.

| Flag | Ý nghĩa |
|---|---|
| `--local` | Dữ liệu trong `<repo>/.office` (chỉ quản lý repo này), tự thêm vào `.gitignore` |
| `--template <key>` | `solo` \| `team` \| `council` \| mẫu tự tạo; không có thì hỏi |
| `--replace` | Thay mô hình hiện có của repo |
| `-y, --yes` | Tự đồng ý tạo kết nối phát hiện được |
| `--home <dir>` | (toàn cục) chọn thư mục dữ liệu |

```text
$ office init ~/code/shop --template team -y
Dữ liệu office: /Users/you/.agent-office (chế độ global)
✓ Đã đăng ký repo demo-shop (/Users/you/code/shop)
✓ Áp mô hình Team: 9 agent
    lead     team-lead          Điều phối, chốt quyết định, báo cáo cho người
    manager  product-manager    Yêu cầu và giá trị cho người dùng
    ...
✓ Kết nối Claude Code CLI: 2.1.281 (Claude Code)
```

| Lệnh | Ý nghĩa |
|---|---|
| `office project add [path] [--name] [--template]` / `list` / `rm <path\|tên>` | Quản lý project (bí danh `repo`); không có path = helper toàn máy |
| `office provider add --kind --name [--base-url] [--api-key-env \| --api-key-stdin]` | Thêm kết nối, tự kiểm tra |
| `office provider list` / `test <tên> [--prompt]` | Liệt kê, kiểm tra, gửi thử |
| `office template list` | Thư viện mô hình mẫu |
| `office export [dir] [--commit] [--file x.json]` | Xuất config (không key) ra thư mục cho git hoặc một file |
| `office import <dir\|file> [--dry-run] [-y]` | Nhập config, luôn xem trước |
| `office backup [--out dir]` | Sao lưu database + khóa mã hóa |

## `office doctor`

Kiểm tra runtime, MCP, quyền read-only, budget, config.

| Flag | Ý nghĩa |
|---|---|
| `--agent <id>` | Chỉ kiểm tra một agent |
| `--fix` | Tự sửa lỗi an toàn (tạo data dir, chạy migration) |

```text
$ office doctor
config     office.config.json hợp lệ (schema v1)                        ✓
storage    sqlite .office/office.db, migration 1/1                      ✓
runtime    claude-code 2.x, đã đăng nhập                                ✓
runtime    api/anthropic: thiếu ANTHROPIC_API_KEY (fallback của director) ⚠
mcp        graylog  12 tool, 12 read-only                               ✓
mcp        metabase 14 tool, 14 read-only                               ✓
worker     graylog-worker tools_allow khớp annotation readOnlyHint       ✓
budget     org $5.00/ngày, đã dùng $0.00                                ✓
notify     discord: DISCORD_WEBHOOK_URL đã set                          ✓
1 cảnh báo, 0 lỗi
```

Exit 3 nếu có lỗi chặn (config sai, không runtime nào khả dụng, worker có tool ghi mà không khai báo `side_effects`).

## `office run`

Bật scheduler, heartbeat, webhook và API.

| Flag | Mặc định | Ý nghĩa |
|---|---|---|
| `--api <addr>` | `127.0.0.1:8787` | REST + SSE + webhook |
| `--workers <n>` | 2 | Số run chạy song song |
| `--once` | tắt | Chạy các run đến hạn rồi thoát (cho cron ngoài) |
| `--only <agent,…>` | tất cả | Giới hạn agent |
| `--dry-run` | tắt | Dùng fake runtime, không tốn token |

```text
$ office run
office 0.1.0 · storefront-v5 · sqlite · api http://127.0.0.1:8787
10:15:02 scheduler  4 lịch, next: tech-lead 10:15:30 (normal)
10:15:30 tech-lead  heartbeat plan → cần graylog delta 09:45–10:15
10:15:41 graylog-worker  ✓ 1,204 log, 37 error (summary lưu wo_01J…)
10:15:58 tech-lead  finding observation (conf 0.3) · next 11:30 (normal) · $0.021
```

## `office status`

| Flag | Ý nghĩa |
|---|---|
| `--since <dur>` | Khoảng finding mới, mặc định `24h` |
| `--watch` | Tự làm mới |

```text
$ office status
Agent            Level     Mode        Next      Runs 24h  Cost 24h  Trạng thái
director         director  -           08:00     3         $0.41     ok
tech-lead        manager   suspicious  10:25     41        $0.88     ok
data-analyst     manager   normal      11:10     12        $0.30     ok
risk-security    manager   normal      12:00     8         $0.19     ok
graylog-worker   worker    -           -         44        $0.12     ok
Tổng hôm nay: $1.90 / $5.00

Finding mới (24h): 6   Incident mở: 1 (inc_01J… "checkout 5xx tăng", awaiting_approval)
```

## `office ask "<câu hỏi>"`

Director điều phối cả phòng trả lời ad-hoc.

| Flag | Ý nghĩa |
|---|---|
| `--managers <id,…>` | Ép manager tham gia |
| `--budget-usd <n>` | Budget cho câu hỏi, mặc định `budget.per_ask_usd` |
| `--no-debate` | Chỉ phân tích độc lập rồi tổng hợp |
| `--follow` | Stream tiến trình |

```text
$ office ask "Vì sao tỉ lệ checkout thành công hôm qua giảm?" --follow
director     chọn: tech-lead, data-analyst, risk-security
data-analyst metabase-worker → conversion theo giờ (wo_01J…)
tech-lead    graylog-worker → lỗi /api/checkout (wo_01J…)
…
Kết luận (tự tin 0.72)
  Lỗi 502 từ proxy API 20:00–21:30 làm checkout thất bại ở VN, US.
Evidence: wo_01J…A (graylog), wo_01J…B (metabase)
Phương án: 1) kiểm tra timeout proxy  2) retry phía client (cần duyệt sửa code)
Rủi ro: dữ liệu Metabase trễ 1 giờ.   Chi phí: $0.34   thread: th_01J…
```

## `office add manager|worker`

| Flag | Ý nghĩa |
|---|---|
| `--id <id>` | Bắt buộc |
| `--role <template>` | Với manager |
| `--runtime`, `--model` | Mặc định theo policy vai trò |
| `--mcp <server>` | Với worker |
| `--tools <a,b>` | Allowlist tool cho worker |
| `--assign <manager,…>` | Gán worker cho manager |

```text
$ office add worker --id sentry-worker --mcp sentry --assign tech-lead
Diff:
  + workers[sentry-worker] runtime claude-code, model claude-haiku-4-5, read_only true
  + managers[tech-lead].workers += sentry-worker
Áp dụng? [y/N] y
✓ Đã ghi. Chạy `office doctor --agent sentry-worker` để kiểm tra.
```

## `office approve <id>` / `office reject <id>`

Duyệt hoặc từ chối `approval_request` hay option của incident.

| Flag | Ý nghĩa |
|---|---|
| `--note "<text>"` | Ghi chú lý do, lưu audit |
| `--list` | Liệt kê đang chờ |

```text
$ office approve --list
apr_01J…  inc_01J…  retry phía client (sửa code, worker code-writer)  hết hạn 18:00
$ office reject apr_01J… --note "đợi sửa proxy trước"
✓ Đã từ chối. Director được thông báo.
```

## `office tune`

Đề xuất chỉnh cơ cấu từ lịch sử vận hành (M5).

| Flag | Ý nghĩa |
|---|---|
| `--window <dur>` | Mặc định `14d` |

```text
$ office tune
Dựa trên 14 ngày:
  ~ risk-security.model  sonnet → haiku   reason: 0 finding severity ≥ medium, chi phí $2.10
  ~ tech-lead.heartbeat.normal.min 30 → 45  reason: 92% heartbeat không có finding
  - workers[metabase-worker] gán thêm tech-lead  reason: 5 lần tech-lead hỏi data-analyst về đơn hàng
[Á]p dụng  [C]hỉnh  [H]ủy ?
```

## `office learn`

Cập nhật baseline và knowledge từ dữ liệu thật (M5).

| Flag | Ý nghĩa |
|---|---|
| `--agent <id>` | Chỉ một manager |
| `--window <dur>` | Mặc định `7d` |
| `--baseline-only` | Không cập nhật knowledge.md |

```text
$ office learn
tech-lead     baseline: error_rate p95 0.8% → 0.6%, log/giờ median 4.1k
              noise: +2 signature (bot crawler 404, redis reconnect)
data-analyst  baseline: conversion median 2.3% (theo giờ)
knowledge.md  +3 mục (từ 2 incident đã resolved)
Áp dụng? [y/N]
```

## `office import senprints <file.json…>`

Chuyển file export agent của senprints-agents (`-export-agents` hoặc `export.json` từ dashboard) thành khối agent trong `office.config.json`. Field được map 1-1 vào `rules` (xem ADR-013). Field không có tương đương được liệt kê để người quyết.

| Flag | Ý nghĩa |
|---|---|
| `--as director\|manager\|worker` | Cấp của agent sau khi import, mặc định `manager` |
| `--role <template>` | Role template gán cho agent |
| `--assign <manager>` | Với worker |

```text
$ office import senprints ./exports/daily-dashboard-changelog-digest.json --as manager --role tech-lead
Map:  instructions, allowed_tools, strict_mode, permission_mode, model, timeout_seconds,
      max_attempts, daily_cost_limit_usd, max_runs_per_hour, notify_on        → rules / agent
      schedule_cron "0 9 * * *"                                                → director.reports (đề xuất)
Bỏ qua: api_key, notify_discord_webhook_url (secret, khai báo lại bằng *_env), team_ids
Diff:
  + managers[daily-dashboard-changelog-digest]
Áp dụng? [y/N]
```

## `office ui`

In hướng dẫn mở dashboard. Nếu `dashboard/.output` đã build, chạy nó như tiến trình con trỏ tới API hiện tại.

```text
$ office ui
API    http://127.0.0.1:8787 (token trong .office/ui-token)
UI     http://127.0.0.1:2704
```

## Lệnh debug

| Lệnh | Ý nghĩa |
|---|---|
| `office worker exec <id> "<query>"` | Chạy một worker ngay, in output JSON |
| `office memory show <agent> [--layer]` | Xem memory hiện hành |
| `office memory compact <agent>` | Ép compaction |
| `office runs [--agent] [--status]` | Liệt kê run |
| `office config validate` | Chỉ validate schema |
