# Cổng MCP chung qua office (bản nháp)

> Trạng thái: đã chốt hướng làm (cổng chung, có chuyển kết nối sẵn có) và phần 1. Phần 2–4 chưa duyệt.

## Mục tiêu

- MCP do office quản lý trọn, mọi thao tác làm trên app:
  - thêm server,
  - đăng nhập (kể cả OAuth),
  - kiểm tra, sửa lỗi.
- Không phụ thuộc vào một AI nào: Claude CLI, Codex CLI, AI qua API, và các cách kết nối AI thêm sau này.
- Thấy rõ mỗi MCP đang được agent nào, AI nào dùng.
- **Chuyển được các kết nối sẵn có** (cấu hình MCP của Claude Code, Codex, `.mcp.json`) thành kết nối chung qua office.
- Là nền cho khu Connector sau này: connector tự code chỉ là thêm tool vào cổng này.

## Hiện trạng

- Mỗi AI tự đọc MCP từ cấu hình riêng của nó:
  - Claude CLI: `~/.claude.json` và `.mcp.json`.
  - Codex: `~/.codex/config.toml`.
- AI qua API (Anthropic, OpenAI) chỉ dùng công cụ của office.
- MCP server riêng của office (`internal/mcpserver`) đã có:
  - chạy qua streamable HTTP,
  - mỗi lượt chạy có token riêng, gắn với một project,
  - Claude Code của người dùng nối vào bằng token cá nhân (ADR-047).
- ADR-088 mới xem được trạng thái kết nối (qua `claude mcp list`), chưa đăng nhập hay sửa được.

## Phần 1: Kiến trúc và dữ liệu (đã chốt hướng)

**Danh sách MCP của office**
- Bảng `mcp_servers` gồm:
  - tên;
  - loại (`http` | `stdio`);
  - URL hoặc lệnh chạy;
  - biến môi trường hoặc header;
  - phạm vi;
  - nguồn gốc (tạo mới, hoặc chuyển từ Claude/Codex/`.mcp.json`).
- Khóa bí mật và token OAuth được mã hóa trong DB bằng khóa sẵn có của office. API luôn trả về giá trị đã che.

**Cổng chuyển tiếp**
- Mỗi MCP có một địa chỉ riêng trên office: `/mcp/s/<tên>`. Office nhận lệnh rồi chuyển tiếp:
  - **HTTP:** gọi thẳng server gốc, tự gắn token.
  - **stdio:** office tự chạy tiến trình (`npx …`) và giữ nó sống trong lúc có người dùng; quá 10 phút không ai dùng thì tắt.
- Có địa chỉ riêng cho từng server nên **tên tool không đổi** (`mcp__context7__…`). Skill và prompt cũ vẫn chạy sau khi chuyển.
- Mỗi lượt chạy chỉ nhận địa chỉ của những MCP được gán cho agent của lượt đó, dùng chung token của lượt:
  - Claude CLI: qua `--mcp-config`.
  - Codex: qua `-c mcp_servers.<tên>.url=…`.
  - AI qua API: office tự gọi tool.
- Mọi lần gọi tool đều đi qua office: nhật ký và quyền duyệt nằm ở một chỗ.

**Câu hỏi còn mở:** MCP của office có chia theo project không, hay chỉ toàn máy rồi gán theo agent?

## Phần 2: Đăng nhập và kiểm tra (chưa duyệt)

Hướng dự kiến:
- **Đăng nhập OAuth theo chuẩn MCP**, office tự làm:
  - tìm thông tin máy chủ ủy quyền (`/.well-known/oauth-protected-resource`, `/.well-known/oauth-authorization-server`);
  - đăng ký client động;
  - PKCE;
  - callback về `/api/mcp/oauth/callback`;
  - tự làm mới token.
- **Server dùng token/API key:** nhập trên form, lưu mã hóa.
- **Kiểm tra kết nối:** office tự làm như một MCP client (`initialize` + `tools/list`), xong trong vài giây và hiện được danh sách tool. Kết quả đẩy qua event như ADR-088.
- **Báo lỗi kèm gợi ý sửa,** ví dụ "máy chưa có `uvx`", "token hết hạn: bấm Kết nối lại".

## Phần 3: Chuyển kết nối sẵn có (chưa duyệt)

Hướng dự kiến:
- Nút **Chuyển vào office** cho từng server trong tab MCP, hoặc chuyển nhiều server một lúc.
- **Cấu hình:** chép từ nguồn (lệnh, URL, biến môi trường, header). Bí mật được chuyển vào DB đã mã hóa.
- **Đăng nhập:** token OAuth mà Claude Code đang giữ thì office không đọc được, nên server cần OAuth phải đăng nhập lại một lần qua office.
  - Connector của claude.ai do tài khoản claude.ai giữ. Office lấy URL rồi tự đăng nhập lại như một MCP HTTP thường.
- **Tránh trùng tool:** sau khi chuyển, office gỡ server khỏi cấu hình gốc (có bản sao trong thùng rác, khôi phục được).
- **Tùy chọn:** thêm mục `office` vào cấu hình Claude Code/Codex cá nhân, để chính người dùng cũng dùng các MCP chung qua office (token cá nhân, ADR-047).

## Phần 4: Gán cho agent/AI, quyền, nhật ký, giao diện (chưa duyệt)

Hướng dự kiến:
- **Gán:** mỗi MCP gán cho agent nào. Mặc định là mọi agent của các project trong phạm vi.
- **Hiện ra ở hai phía:**
  - Trên MCP: "đang gắn cho: agent X (Claude CLI), agent Y (Codex)…".
  - Trên trang agent: danh sách MCP của agent đó.
- **Quyền theo tool:** đọc thì tự chạy, tool có ghi thì cần duyệt, theo hệ quyền đang có (ADR-074).
- **Nhật ký:** mỗi lần gọi tool có ai gọi, tool nào, thời gian, kết quả ngắn; hiện trong Nhật ký và thống kê theo MCP.
- **Giao diện:** khu quản lý MCP (sau này thành Connector) gồm danh sách, trạng thái, nút Kết nối/Kiểm tra/Chuyển vào office, chi tiết tool, gán agent.

## Ngoài phạm vi (lần này)

- Connector tự code (Messenger, Zalo OA, Slack, Email): làm sau, trên nền cổng này.
- Đồng bộ hai chiều với cấu hình Claude/Codex sau khi đã chuyển.
