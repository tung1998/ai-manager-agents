---
key: knowledge-base-audit
name: Audit knowledge base
category: Knowledge
description: Tìm nội dung lỗi thời, trùng lặp hoặc khó tìm và lập kế hoạch cải thiện knowledge base.
input: Knowledge base hoặc bộ tài liệu cần audit
inputs:
  - { key: context, description: "Danh mục, usage, owner, ngày cập nhật và feedback", required: true }
outputs:
  - { key: findings, description: "Gap, stale content, duplicate và evidence", required: true, type: list }
  - { key: plan, description: "Ưu tiên sửa, archive, merge và owner", required: true, type: list }
roles:
  - { key: librarian, name: Quản trị nội dung, hint: taxonomy, findability, lifecycle }
  - { key: users, name: Người dùng, hint: search intent, unanswered questions, differ_from: [librarian] }
  - { key: subject, name: Chuyên gia nội dung, hint: accuracy, source, ownership }
parallel: [[librarian, users], [subject]]
limits: { rounds: 2, turns: 8, timeout: 2h }
brief: [context]
---
1. Đối chiếu usage với freshness và độ chính xác; không xóa nội dung nếu chưa có owner xác nhận.
2. Nhóm gap theo impact và chi phí, đề xuất canonical source.
3. Gọi `workflow_done` với findings và plan.
