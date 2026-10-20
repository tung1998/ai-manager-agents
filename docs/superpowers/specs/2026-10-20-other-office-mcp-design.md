# MCP `other_office`: một cửa duy nhất tới office khác

Ngày: 20/10/2026. Thay phần "Liên kết", "Đọc và tìm" phía agent của ADR-167.

## Vì sao

Office khác (ADR-167) khó dùng. Agent và trợ lý office không biết office kia ở đâu: phần office khác nằm lẫn trong `send_to_chat`, `read_link`, `search_history`, chỉ hiện ra khi agent đoán đúng tên `<office>/<project>`. Trong thực tế, trợ lý office thử SSH, lục `office.db`, gọi API bên kia (401) thay vì dùng kết nối đã ghép.

Người dùng muốn **một MCP duy nhất `other_office`**: nhìn vào là thấy các office đang kết nối, project nào dùng được, và mọi việc làm được với các project đó. Thông tin kết nối (địa chỉ, khóa) là dữ liệu bên trong MCP này, agent không cần biết.

## Mục tiêu

- Agent thấy MCP `other_office` (tool `mcp__other_office__*` với Claude Code/Codex/Gemini CLI; cùng bộ tool cho run qua API).
- Tool tối thiểu:
  - **`offices`**: các office đang ghép nối (tên, địa chỉ, dùng lần cuối) và các project của từng office mà agent này được dùng (id, tên, mô tả).
  - **`send`**: giao việc cho project X của office Y. Không có `chat` thì mở chat mới; có `chat` (id hoặc link) thì gửi tiếp vào chat đó. Câu trả lời tự báo lại về chat gốc như hiện nay.
  - **`read_chat`**: đọc chat của office kia theo id hoặc link (tùy chọn tin nhắn `m`).
  - **`search`**: tìm chat trong project X của office Y (query, days, author).
- Tham số dùng **tên** office và project (chấp nhận cả id), không dùng `peer:<id>/<id>`.
- Quyền:
  - **Agent của project:** chỉ thấy và thao tác các project bên kia đã chọn trong *Cài đặt project → Quyền & lệnh → Project liên kết* (nhóm "Office khác", dữ liệu `Policy.LinkedProjects` `peer:` giữ nguyên). `send` vẫn theo quyền `chat.cross` và giới hạn `relay_hops` như `send_to_chat`.
  - **Trợ lý office:** thấy mọi office đã ghép nối và mọi project các office đó mở cho mình, không cần liên kết. `send` theo chế độ trợ lý (ADR-059): chế độ "answer" chỉ được `offices`/`read_chat`/`search`. Mọi tài khoản chat với trợ lý đều giao việc được (không chỉ admin), dù bên kia nhận tin dưới tên người đã ghép nối (người dùng đã chọn).

## Không làm

- Không đổi giao thức giữa hai office (`/api/peer/*`, khóa `ofp_`, mã mời `ofi_`), không đổi phía nhận (tin vào chat như người đã ghép nối, tự báo lại, duyệt thẻ ở office đó).
- Không đổi trang *Office khác* (ghép nối, chọn project chia sẻ).
- Không thêm thao tác mới ngoài 4 tool trên (đọc code, git, tiến trình bên kia… để sau, cần thiết kế quyền riêng).
- `other_office` không phải MCP server người dùng thêm/xóa trong trang MCP; nó là MCP có sẵn của office như `office`.

## Gỡ phần cũ

- `send_to_chat`, `read_link`, `search_history` thôi xử lý office khác (`internal/officetools/officetools.go:851`, `readlink.go:22`, `internal/actions/actions.go:741` `peerLink`). Gọi với tên `<office>/<project>` hay link chat của office kia thì trả lỗi ngắn chỉ sang `mcp__other_office__*`.
- Danh sách "Linked projects" trong prompt agent không còn liệt kê project office khác theo dạng `<office>/<project>`; thay bằng một dòng: dùng MCP `other_office` (chỉ khi project có liên kết sang office khác, hoặc là trợ lý office và có ít nhất một office ghép nối).
- Tin "↩ Báo lại" từ office kia vẫn mang link tuyệt đối; agent đọc bằng `read_chat`.

## Ràng buộc

- MCP chung server với `office` (`internal/mcpserver`), cùng token theo lượt chạy và cùng scope (`officetools.Scope`). Đường dẫn riêng, đặt tên server `other_office` trong cấu hình MCP của run (`internal/chat/run_claude.go:535`, `run_codex.go:49`, `run_gemini.go:47`) và trong bộ tool của run qua API.
- Chỉ đưa `other_office` vào run khi agent có ít nhất một project office khác dùng được (tránh tool rỗng).
- Mọi lệnh gọi sang office kia đi qua `internal/peer` như hiện nay (danh sách domain đã ghép, không theo redirect, giới hạn tần suất).
- Thêm ADR-169 vào `docs/DECISIONS.md`, sửa ADR-167 trỏ sang; cập nhật `docs/INTERFACES.md` nếu có liệt kê tool MCP; prompt trợ lý (`prompts` `assistant/instructions`) nhắc: việc với office khác thì dùng `other_office`, không SSH hay gọi API trực tiếp.
- Test: phạm vi theo liên kết (agent thường không thấy project chưa liên kết; trợ lý thấy hết), `send` qua hai office test như `internal/api/peers_test.go`, lỗi chỉ hướng của tool cũ.

## Quyết định của người viết (người dùng kiểm lại)

- `read_chat` của agent project: chỉ chat thuộc project đã liên kết, cộng các chat mà chính project này từng giao việc sang (để đọc được tin "↩ Báo lại" kể cả khi liên kết đã bỏ). Office kia vẫn tự kiểm như hiện nay.
- "Dùng lần cuối" trong `offices` giữ nghĩa hiện tại (`Peer.LastUsedAt`: lần office kia gọi sang mình), không ghi thêm mỗi lần mình gọi đi.
- Thẻ đang chờ duyệt có `args.peer` từ trước vẫn thực hiện được; chỉ đường vào cũ trả lỗi chỉ hướng.

## Để người làm tự quyết

- Tên chính xác của tham số, định dạng kết quả văn bản.
- `send` đi qua thẻ duyệt (action) như `send_to_chat` hay chạy thẳng: giữ đúng hành vi hiện tại của `send_to_chat`.
- Tách code: package mới hay thêm vào `officetools`.
