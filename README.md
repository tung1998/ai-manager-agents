# agent-office

Framework multi-agent mô phỏng một phòng ban: **Director → Managers → Workers** giám sát và phân tích hệ thống phần mềm của một project, trao đổi và phản biện qua blackboard, áp dụng cho project mới chỉ bằng config.

> Dự án độc lập, xây từ đầu. `senprints-agents` chỉ dùng để tham khảo ý tưởng, không extend và không copy code.
>
> Trạng thái: **M0 đang làm**. Đã có server Go, đăng nhập tài khoản và dashboard quản trị. Pilot đầu tiên: `storefront-v5`.

## Chạy thử

Cần Go ≥ 1.27, Node 22, pnpm.

```bash
make build                                   # bin/office
./bin/office init --local --template team    # đăng ký repo hiện tại, chọn mô hình
./bin/office user create --email you@company.com --name "Bạn" --role admin
make ui-install && make ui-build             # build dashboard
./bin/office run                             # API :8787 + dashboard http://localhost:2704
```

`office run` là supervisor: chạy server (`office serve`) và dashboard, tự bật lại khi dừng, và
áp dụng **Quản trị → Cập nhật office** (build lại từ mã nguồn, tự quay về bản cũ nếu bản mới lỗi).
Khi sửa giao diện: `make dev-ui` (hot reload, cổng 2704) cùng `make dev-api` (chỉ server).

Mở http://localhost:2704, đăng nhập bằng tài khoản vừa tạo. Trang Tổng quan có checklist: Kết nối AI → Repo → Mô hình.

Cài trên máy để quản lý nhiều repo: bỏ `--local`, dữ liệu nằm ở `~/.agent-office`, rồi `office init` trong từng repo hoặc `office repo add <path>`.

Chạy bằng Docker:

```bash
cp .env.example .env
docker compose up -d --build
docker compose exec office office user create --email you@company.com --role admin
```

Test: `make test` (Go) và `make ui-build` (typecheck + build dashboard).

## Tài liệu

| File | Nội dung |
|---|---|
| [docs/PLAN.md](docs/PLAN.md) | Tổng quan, stack, giả định, milestone M0–M5, rủi ro |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Thành phần, luồng dữ liệu, sơ đồ Mermaid |
| [docs/DATA_MODEL.md](docs/DATA_MODEL.md) | Schema DB: memory, blackboard, runs, incidents, costs |
| [docs/INTERFACES.md](docs/INTERFACES.md) | RuntimeAdapter, StorageAdapter, NotifyAdapter, output schema |
| [docs/CLI.md](docs/CLI.md) | Đặc tả lệnh `office` |
| [docs/UI.md](docs/UI.md) | Màn hình dashboard và API |
| [docs/DECISIONS.md](docs/DECISIONS.md) | ADR |
| [schema/office.config.schema.json](schema/office.config.schema.json) | JSON Schema config |
| [examples/office.config.example.json](examples/office.config.example.json) | Config pilot storefront-v5 |
| [templates/roles/](templates/roles/) | Prompt khung theo vai trò |

## Cây thư mục

```text
agent-office/
├── cmd/office/              # entry point binary `office`
├── internal/
│   ├── api/                 # REST + SSE + webhook ingress
│   ├── audit/               # log append-only
│   ├── blackboard/          # threads, findings, evidence
│   ├── budget/              # guard chi phí, cost ledger
│   ├── config/              # load/validate/ghi office.config.json
│   ├── debate/              # state machine debate
│   ├── heartbeat/           # vòng heartbeat manager
│   ├── initscan/            # quét repo cho `office init`
│   ├── mcp/                 # registry MCP, phân loại read-only
│   ├── memory/              # 3 lớp memory + compaction
│   ├── notify/              # Discord/Telegram/Slack
│   ├── orchestrator/        # chạy một run, quyết định escalate
│   ├── runtime/             # adapter claude-code/api/codex/gemini + Router
│   ├── scheduler/           # ticker, claim, lease
│   ├── storage/             # SQLite + Postgres
│   └── worker/              # strict MCP, validate output
├── migrations/{sqlite,postgres}/
├── plugins/                 # runtime/notify/storage/trigger/role ngoài core
├── dashboard/               # Nuxt 4 + @nuxt/ui
├── schema/                  # JSON Schema config
├── examples/                # config mẫu
├── templates/roles/         # prompt khung
└── docs/
```

Mỗi thư mục có README ngắn mô tả trách nhiệm, phụ thuộc và milestone.
