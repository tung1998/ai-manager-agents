# Quy trình: cách các agent phối hợp (thay mô hình tổ chức)

## Mục tiêu

- **Skill** là cách *một* agent làm việc. **Quy trình** là cách *nhiều* agent phối hợp: ai làm vai nào, giao gì cho nhau, dừng ở đâu.
- Quy trình có **thư viện** (mẫu dùng chung) và **bản riêng của từng project** (chép khi cài, không kế thừa, như ADR-015).
- Ba quy trình cơ bản: **Giao lại** (handoff), **Cố vấn** (advisor), **Hội đồng** (committee). Về sau có quy trình nâng cao (có cổng duyệt, cổng test).
- **Bỏ mô hình tổ chức.** Mô hình chỉ là danh sách agent cộng phần "phối hợp" chưa từng được thi hành:
  - `reports_to` không được dùng lúc chạy;
  - quorum và veto chỉ được lưu và kiểm tra (`internal/orgmodel/validate.go`);
  - `tier=lead` chỉ để chọn agent mặc định của chat (`internal/chat/engine.go:771`).

  Phần phối hợp chuyển thành quy trình, và được **thi hành thật**.

## Bài học phải giữ

1. **Không có lối vào riêng.** "Việc" bị bỏ (ADR-055 → 057) vì nó có chỗ chạy, chỗ duyệt và chỗ xem riêng. Quy trình chạy **trong chat**, gọi bằng `/tên`, kết quả nằm trong chat. Thư viện và tab Skills & MCP chỉ dùng để quản lý mẫu.
2. **Không DSL đồ thị bước.** Agent điều phối (LLM) tự điều hành theo phần thân của quy trình. Engine chỉ thi hành các **ràng buộc** ở frontmatter: vai trò, quyền, giới hạn, khung giao việc, cổng.
3. **Giao kết quả và contract, không giao cách làm** (bài "10 anti-pattern", mục 1–3). Khung giao việc bắt buộc tách *kết quả cần đạt / ràng buộc đã kiểm chứng / phương án đang thử*. Phần hướng dẫn cấm ghi chi tiết tới mức file hay hàm.
4. **Mẫu dùng vai trò, không ghi tên agent.** Vai trò được gán với agent khi cài hoặc khi chạy.

## Định dạng

Mỗi quy trình là một file `.md`: frontmatter YAML do engine ép, phần thân là hướng dẫn cho agent điều phối.

```markdown
---
key: hoi-dong
name: Hội đồng
description: Hai agent khác hãng model phân tích độc lập rồi thống nhất một kế hoạch. Dùng khi bế tắc, sửa mãi không xong, bài toán khó.
input: Vấn đề cần phân tích
roles:
  - key: a
    name: Thành viên A
    hint: lập kế hoạch, phân tích nguyên nhân gốc
    access: analyze
  - key: b
    name: Thành viên B
    hint: phản biện, tìm rủi ro
    access: analyze
    differ_from: [a]
parallel: [[a, b]]
limits: { rounds: 3, turns: 8, timeout: 60m }
brief: [outcome, constraints, current_option, tried, done_when]
---
1. Viết đề bài ở mức vấn đề (không đề xuất cách làm), giao cùng lúc cho a và b.
2. Có cả hai kết quả thì so sánh. Chỗ bất đồng: gửi lập luận của bên này cho bên kia (`workflow_send`).
3. Dừng khi hai bên thống nhất hoặc hết số vòng. Báo người dùng: kết luận chung, chỗ từng bất đồng và cách giải quyết.
```

| Trường | Ý nghĩa | Engine ép thế nào |
|---|---|---|
| `key`, `name`, `description` | Định danh và mô tả. `key` cũng là lệnh `/key` | `key` không trùng skill hay quy trình khác của project |
| `input` | Mô tả phần chữ sau lệnh | Thiếu phần chữ thì nhắc cách gõ, như lệnh bot |
| `roles[].hint` | Sở trường cần có, dùng để gợi ý gán agent | — |
| `roles[].access` | Trần quyền của vai: `analyze` (chỉ đọc), `propose`, `edit` (worktree riêng) | Lượt của vai chạy với `WithCeiling` theo mức này, bất kể quyền của agent. `analyze` = `perm.Read`, không sửa file |
| `roles[].differ_from` | Vai phải dùng kết nối AI **khác họ** (Anthropic ≠ OpenAI ≠ …) | Không đủ kết nối khác họ: thẻ ghi cảnh báo và chạy tiếp, trừ khi `strict: true` thì từ chối |
| `parallel` | Các nhóm vai được giao cùng lúc | Vai trong cùng một nhóm phải là các agent khác nhau. Một agent chỉ làm một việc nền trong một chat (`members.go:181`) |
| `limits` | `rounds` (số lần gửi tiếp cho một vai), `turns` (tổng lượt của mọi vai), `timeout`, `budget_usd` (tùy chọn) | Vượt giới hạn thì tool trả lỗi; hết `timeout` thì dừng mọi lượt nền của lần chạy |
| `brief` | Các mục bắt buộc của bản giao việc | `workflow_delegate` nhận `brief` dạng object; thiếu mục bắt buộc thì trả lỗi |
| `gates` | (giai đoạn 3) Điểm phải dừng: `approve` (người duyệt), `check` (lệnh kiểm tra phải đạt) | Xem giai đoạn 3 |

Các mục `brief` có sẵn:
- `outcome`: kết quả người dùng cần, không phải giải pháp;
- `constraints`: ràng buộc đã kiểm chứng;
- `current_option`: phương án đang thử, được phép phản biện;
- `tried`: đã thử gì, vì sao bỏ;
- `files`: file liên quan, chỉ đường dẫn;
- `done_when`: tiêu chí xong;
- `must_not`: điều cấm.

Engine dựng các mục thành markdown theo thứ tự cố định. Với vai `analyze`, engine luôn nối thêm câu: "Chỉ phân tích, không sửa file."

## Nơi lưu

- **Thư viện:** file `<office>/library/workflows/<key>.md`, cùng nếp `library/{skills,agents,mcp}` (ADR-024).
  - Ba mẫu cơ bản nhúng trong binary, seed nếu chưa có, không ghi đè bản đã sửa. Có "Khôi phục mặc định".
- **Project:** bảng `workflows` (migration mới):
  - `id`, `project_id`, `key`, `name`, `body` (cả file);
  - `source_key`, `source_hash` (mẫu đã chép từ đâu, để báo "mẫu có bản mới");
  - `bindings` (JSON vai → agent_id);
  - `enabled`, `created_at`, `updated_at`.

  Lưu trong DB để sửa được trên dashboard và qua `propose_change` (đăng ký resource `workflow` trong config registry, ADR-045). Có lịch sử thay đổi như automation.
- **Cài vào project** = chép file. Khi cài, office gợi ý gán vai bằng cách so `hint` với `role`/`description` của agent (model mức fast). Người dùng sửa được.
- **Vai chưa gán** thì lúc chạy agent điều phối tự chọn trong các agent của project, có báo người dùng. Engine vẫn kiểm tra `access`, `differ_from`, `parallel`.

## Chạy

### Gọi
- Trong chat (web, Discord, Telegram): tin bắt đầu bằng `/key …`. Office chặn trước khi gửi cho CLI, tạo **lần chạy** (`workflow_runs`), rồi chạy lượt của agent đang trả lời trong chat. Agent này là **agent điều phối**.
- Tự động hóa và lệnh bot: nội dung gửi agent bắt đầu bằng `/key` thì đi đúng đường đó. Không thêm hành động mới (ADR-057 giữ nguyên: chỉ Chat | Script).
- Agent tự đề xuất một quy trình: chỉ nhắc người dùng gõ `/key`. Lần chạy luôn do người hoặc tự động hóa khởi động.

### Agent điều phối
- System prompt của lượt điều phối có thêm: phần thân quy trình, bảng vai (vai → agent đã gán, quyền, kết nối AI), giới hạn còn lại. Dặn: giao bằng `workflow_delegate`, không tự làm phần việc của vai.
- Công cụ, chỉ có trong lượt thuộc một lần chạy:
  - `workflow_delegate(role, brief, agent?)`: giao cho vai. Gọi nhiều lần trong một lượt thì các vai cùng nhóm `parallel` chạy song song.
  - `workflow_send(role, message)`: gửi tiếp cho vai đã giao, **giữ phiên của vai** (advisor dùng lâu, hội đồng trao đổi qua lại). Mỗi lần tính một `round`.
  - `workflow_done(summary)`: kết thúc lần chạy. Kết luận là câu trả lời cuối trong chat.
- `delegate` thường vẫn dùng được ngoài quy trình. Trong lần chạy thì nó bị ẩn, để mọi việc giao đều đi qua ràng buộc.

### Vai làm việc
- Mỗi lượt của vai là một lượt nền trong chat, dùng lại `startTurn` (`internal/chat/members.go:157`):
  - `ceiling` theo `access`;
  - worktree: vai `edit` dùng worktree riêng của agent trong chat (`chatTree`); vai `analyze` không cần worktree;
  - job con có `parent_job_id` = job của lượt điều phối, nên chi phí gom đúng.
- **Phiên của vai:** lần giao đầu mở phiên mới (như ADR-079). `workflow_send` resume đúng phiên đó (`workflow_run_roles.session_id`). Phiên quá 70% thì tự compact (ADR-079).
- **Gọi lại agent điều phối (gom kết quả):** không gọi lại sau *từng* vai. Gọi một lần khi mọi vai đã giao trong lượt đều xong (hoặc lỗi, hoặc hết giờ), kèm kết quả của tất cả. Cùng cơ chế với `prev.background` ở `nextTurn`, chỉ thêm bộ đếm theo lần chạy.
- Người dùng nhắn vào chat khi lần chạy đang chạy: tin này là chỉ đạo, đưa vào lượt điều phối kế tiếp. Nút Dừng của chat (`StopAll`) dừng cả lần chạy.

### Bản ghi
- `workflow_runs`: `id`, `project_id`, `conversation_id`, `workflow_id`, `workflow_key`, `body_hash` (bản quy trình lúc chạy), `status` (running | done | failed | stopped), `turns`, `cost_usd`, `started_at`, `finished_at`, `result`, `coordinator_job_id`.
- `workflow_run_roles`: `run_id`, `role`, `agent_id`, `session_id`, `rounds`, `status`, `cost_usd`.
- **Thẻ quy trình trong chat** (tin `role=workflow`, nội dung là `run_id`). Dashboard vẽ thẻ từ `workflow_runs`, cập nhật bằng event (ADR-078): tên quy trình, vai → agent → trạng thái, vòng, lượt còn lại, chi phí, nút Dừng. Bot gửi bản chữ ngắn của thẻ theo kiểu trả lời của bot (ADR-097).
- Trang Job: lần chạy hiện là một dòng loại "Quy trình", mở ra thấy các job con.

## Ba quy trình cơ bản

| Quy trình | Vai | Điểm chính |
|---|---|---|
| **Giao lại** `/giao-lai` | `nguoi-nhan` (`edit`) | Agent điều phối viết bản giao đủ mục (`outcome`, `constraints`, `current_option`, `tried`, `files`, `done_when`), giao rồi `workflow_done` ngay, không chờ. Kết quả của người nhận vẫn báo lại vào chat như delegate thường |
| **Cố vấn** `/co-van` | `co-van` (`analyze`, `differ_from: [dieu-phoi]`) | Hỏi rõ câu hỏi, những gì đã xét và đã loại, yêu cầu "khuyến nghị kèm lý do". Tổng hợp: ý kiến cố vấn + khuyến nghị của mình. Có thể `workflow_send` hỏi tiếp trong cùng lần chạy |
| **Hội đồng** `/hoi-dong` | `a`, `b` (`analyze`, khác họ, song song) | Như ví dụ ở trên; tối đa 3 vòng trao đổi |

`dieu-phoi` là tên vai có sẵn chỉ agent điều phối, dùng được trong `differ_from`.

## Bỏ mô hình tổ chức

### Đi đâu
| Hiện tại | Sau |
|---|---|
| `agents.org_model_id` | `agents.project_id` (lấy từ `org_models.repo_id`) |
| Lead đầu tiên = agent mặc định của chat | `repos.default_agent_id` |
| `tier`, `reports_to` | Bỏ khỏi giao diện và prompt. `role` + `instructions` giữ nguyên nội dung |
| `governance.council` (quorum, veto) | Quy trình **Hội đồng N bên** cài sẵn cho project: một vai cho mỗi lead cũ, `quorum` và `veto` vào frontmatter (cần trường `vote` ở giai đoạn 3; trước đó ghi trong phần thân) |
| `governance.hierarchy`, `notes` | `notes` chép vào `instructions` của agent mặc định |
| Mẫu mô hình (Solo, Team, Tam quyền) | **Gói khởi tạo** `templates/packs/*.json`: danh sách agent + key các quy trình cài sẵn. Chỉ dùng lúc tạo project, chép xong là xong |
| Trình soạn mô hình bằng chat (`chat/template_guide.go`) | Soạn quy trình bằng chat, như soạn skill (ADR-062) |
| Lịch sử mô hình (`revisions`) | Lịch sử theo từng agent và từng quy trình (nhật ký ADR-045) |
| Xuất/nhập (`transfer`) | Xuất agent + quy trình của project |

### Thứ tự gỡ (không làm gãy lúc nào)
1. Thêm `agents.project_id` và `repos.default_agent_id`, điền từ dữ liệu cũ. `Engine.Agents` đọc theo `project_id` (`internal/chat/engine.go:442`). Lỗi `ErrNoModel` thành "project chưa có agent".
2. Dashboard: tab Agents của project liệt kê thẳng agent, thêm, sửa, chọn mặc định. Bỏ trang "Mẫu mô hình tổ chức", đưa gói khởi tạo vào Thư viện. `office init` và Thêm project chọn gói.
3. Migration chuyển `governance.council` thành quy trình đã cài.
4. Gỡ `internal/orgmodel`, `api/org.go`, phần mô hình trong `setup`, `transfer`, `agentinfo`; xóa bảng `org_models` và cột `tier`, `reports_to`, `org_model_id`. Gỡ `storage.Task*` còn sót nếu không còn ai đọc (dữ liệu Việc cũ: xem ADR-057).

## Giai đoạn

| Giai đoạn | Nội dung | Xong khi |
|---|---|---|
| **1. Quy trình chạy được** | Định dạng + parser + kiểm tra, bảng `workflows`/`workflow_runs`/`workflow_run_roles`, gọi `/key`, 3 công cụ, ép `access`/`differ_from`/`parallel`/`limits`/`brief`, gom kết quả, thẻ trong chat, 3 mẫu cơ bản | `/hoi-dong` trên một project thật: 2 agent khác họ chạy song song, không sửa file, trao đổi ≤ 3 vòng, có thẻ và chi phí |
| **2. Thư viện + project** | Thư viện file, cài/gỡ/khôi phục, gán vai, sửa trên dashboard, resource `workflow` cho `propose_change`, soạn bằng chat, báo "mẫu có bản mới" | Cài một mẫu vào 2 project, sửa một bên không ảnh hưởng bên kia |
| **3. Bỏ mô hình tổ chức** | Bước 1–4 ở trên, gói khởi tạo | Không còn code, bảng hay trang nào về mô hình; project cũ chạy như trước |
| **4. Quy trình nâng cao** | `gates` (`approve` qua thẻ duyệt sẵn có, `check` qua lệnh kiểm tra của project), `vote` (quorum/veto). Mẫu: **Làm tính năng** (kế hoạch → hội đồng duyệt kế hoạch → làm trong worktree → review bằng model khác họ → test xanh → người duyệt), **Sửa bug**, **Review PR**, **Viết nội dung** (nháp → phản biện → duyệt → đăng) | Mỗi mẫu chạy được trên một project thật |

Burn giữ nguyên. Chuyển Burn thành quy trình chỉ xét sau giai đoạn 4, khi đã có `gates` và vòng lặp.

## Không làm
- Đồ thị bước, rẽ nhánh, biến trong frontmatter.
- Tab hay trang riêng để *chạy* quy trình.
- Quy trình gọi quy trình khác; quy trình chạy qua nhiều project.
- Lịch chạy trong quy trình (dùng tự động hóa).

## Kiểm thử
- Parser: frontmatter hợp lệ/không hợp lệ, `key` trùng skill, vai trùng, `parallel` tham chiếu vai không có.
- Engine (giả lập runner như `chat` test hiện có):
  - vai `analyze` chạy với trần `perm.Read`;
  - `differ_from` từ chối hoặc cảnh báo khi cùng họ;
  - hai vai song song cùng một agent thì bị từ chối;
  - hết `turns`/`rounds` thì tool trả lỗi;
  - thiếu mục `brief` thì trả lỗi;
  - gom kết quả: điều phối được gọi lại đúng một lần khi cả hai vai xong, kể cả khi một vai lỗi;
  - Dừng chat thì dừng mọi lượt của lần chạy, `workflow_runs.status=stopped`.
- Migration giai đoạn 3: dữ liệu mẫu của 3 kiểu mô hình (solo, team, council) chuyển đúng agent, agent mặc định, quy trình hội đồng.

## Quyết định cần ghi (ADR)
- ADR-098: Quy trình (giai đoạn 1–2).
- ADR-099: Bỏ mô hình tổ chức, gói khởi tạo. Supersedes ADR-015 (phần mô hình).
- ADR-100: Quy trình nâng cao (`gates`, `vote`).
