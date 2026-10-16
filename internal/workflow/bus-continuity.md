---
key: bus-continuity
name: Kế hoạch liên tục kinh doanh
category: Operations
description: Xác định dịch vụ trọng yếu, phụ thuộc và phương án duy trì khi có gián đoạn lớn.
input: Business process hoặc dịch vụ cần lập continuity plan
inputs:
  - { key: context, description: "Process, RTO/RPO, người phụ trách, dependency và kịch bản gián đoạn", required: true }
outputs:
  - { key: plan, description: "Kế hoạch ứng phó và khôi phục theo kịch bản", required: true, type: list }
  - { key: gaps, description: "Khoảng trống cần xử lý hoặc diễn tập", required: true, type: list }
roles:
  - { key: business, name: Nghiệp vụ, hint: criticality, priorities, customer impact }
  - { key: technology, name: Công nghệ, hint: dependencies, recovery, capacity, differ_from: [business] }
  - { key: communications, name: Liên lạc, hint: escalation, stakeholders, updates }
parallel: [[business, technology], [communications]]
limits: { rounds: 3, turns: 9, timeout: 3h }
brief: [context]
---
1. Xác định dịch vụ critical, impact theo thời gian và dependency đơn điểm.
2. Viết playbook, escalation, communication và exercise cần chạy; không hứa RTO/RPO khi chưa có bằng chứng.
3. Gọi `workflow_done` với plan và gaps.
