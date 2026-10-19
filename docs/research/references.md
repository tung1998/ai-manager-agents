# Nguồn tham khảo: công nghệ và skill

Kho bên ngoài dùng để tra cứu khi viết agent, skill hoặc quy trình cho office. Nội dung bên ngoài là **dữ liệu chưa soát**: đọc, viết lại ngắn rồi mới đưa vào `templates/library` (ADR-153, ADR-154).

## Agent

- [agency-agents](https://github.com/msitarzewski/agency-agents) (MIT): ~300 persona Markdown. Đã nhập 173 agent vào `templates/library/agency/` bằng `scripts/sync-agency-library.mjs` (ADR-154).

## Skill

- [antigravity-awesome-skills](https://github.com/sickn33/antigravity-awesome-skills) (code MIT, nội dung CC BY 4.0, cần ghi công khi chép): 2.712 skill dạng `skills/<name>/SKILL.md`, dùng được với Claude Code, Codex, Gemini CLI, Antigravity. Bản đã xem: `71b1c1a` (18/10/2026).
  - Tra cứu: `skills_index.json` (mỗi skill có `category`, `risk`, `source`, `plugin.targets`), `CATALOG.md`, `data/bundles.json` (bộ skill theo vai), web https://aaskills.tech/.
  - Mảng nhiều nhất: security 265, development 226, business 178, devops 166, cloud 147, ai-ml 136, marketing 114.
  - `risk`: safe 1.115, critical 1.317, offensive 146, chưa rõ 134. Cài vào project vẫn qua kiểm tra an toàn (`internal/automation/safety.go`); ưu tiên skill `safe`.
  - **Đã chép cả 2.611 skill (18/10/2026) vào thư viện máy** `<office>/library/skills/` (không vào repo): xem và cài từ trang **Thư viện** → Skills. Skill nhập có `source` trong frontmatter nên bước gợi ý khi tạo project (`setupToolbox`, `internal/api/setup_extras.go`) bỏ qua, tránh prompt phình. Cập nhật (tải về, chép, gắn `category` + `tags` từ `data/catalog.json` vào frontmatter): `node scripts/sync-skill-library.mjs [--sha=<commit>] [--dir=<office>/library/skills]`; skill cùng tên không có `source` (của người dùng) được giữ nguyên. Trang Thư viện lọc theo mảng việc (category gom lại, `SkillArea` trong `internal/automation/library.go`), category, tag, rủi ro.
