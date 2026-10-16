---
key: support-escalation
name: Xử lý escalation khách hàng
category: Customer
description: Tổng hợp case khó, xác định impact và điều phối giải pháp cùng cập nhật khách hàng trung thực.
input: Case hoặc escalation cần xử lý
inputs:
  - { key: context, description: "Lịch sử case, customer goal, SLA, bằng chứng và hành động đã thử", required: true }
outputs:
  - { key: diagnosis, description: "Tóm tắt vấn đề, impact và điều chưa biết", required: true }
  - { key: next_actions, description: "Owner, bước an toàn và nội dung cập nhật", required: true, type: list }
roles:
  - { key: support, name: Hỗ trợ, hint: "timeline, empathy, SLA, customer goal" }
  - { key: technical, name: Kỹ thuật, hint: "reproduction, logs, workaround", differ_from: [support] }
  - { key: account, name: Account, hint: "expectation, communication, relationship" }
parallel: [[support, technical], [account]]
limits: { rounds: 3, turns: 10, timeout: 3h }
brief: [context]
---
1. Dựng timeline và xác minh impact; bảo vệ dữ liệu riêng tư trong mọi tóm tắt.
2. Tách workaround khỏi fix, nêu owner và mốc cập nhật không hứa quá bằng chứng.
3. Gọi `workflow_done` với diagnosis và next_actions.
