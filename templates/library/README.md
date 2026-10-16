# templates/library

Thư viện khởi đầu của office gồm agent và skill. Agent là một file Markdown, frontmatter của office ở đầu (`key`, `name`, `role`, `description`, `category`, `model_tier`, `level`, `featured`, `source`, `emoji`), phần còn lại là hướng dẫn. Danh sách chỉ đọc frontmatter; hướng dẫn đọc khi mở một agent.

- `office/`: viết cho office (tiếng Việt, ngắn), hiện ở mục **Đề xuất**. Thêm mẫu: tạo file `<key>.md`, `go test ./internal/team/` kiểm.
- `agency/`: nhập từ [agency-agents](https://github.com/msitarzewski/agency-agents) (MIT, © 2025 AgentLand Contributors) bằng `node scripts/sync-agency-library.mjs [sha]`, đã bỏ code mẫu, quyền `read`. Không sửa tay: lần đồng bộ sau sẽ ghi đè.
- `skills/`: các skill nền tảng được seed vào thư viện machine-wide khi office khởi động lần đầu; mỗi skill có `SKILL.md` với frontmatter `name` và `description`. Skill chỉ được seed khi chưa có cùng tên, nên chỉnh sửa của người dùng không bị ghi đè.

`category`: `code`, `design`, `plan`, `customer`, `content`. Không agent nào được có `full_access`/`extra_dirs` (ADR-074).
