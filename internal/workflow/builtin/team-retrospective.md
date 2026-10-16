---
key: team-retrospective
name: Retrospective đội nhóm
category: People
description: Biến dữ liệu và phản hồi của đội thành vài cải tiến cụ thể, an toàn và có người chịu trách nhiệm.
input: Kỳ làm việc hoặc sự kiện cần retrospective
inputs:
  - { key: context, description: "Mục tiêu, timeline, metrics, feedback và incident", required: true }
outputs:
  - { key: themes, description: "Chủ đề và evidence", required: true, type: list }
  - { key: actions, description: "Cải tiến, owner và cách đo", required: true, type: list }
roles:
  - key: facilitator
    name: Điều phối
    hint: psychological safety, themes, neutrality
  - key: delivery
    name: Delivery
    hint: flow, blockers, quality
    differ_from: [facilitator]
  - key: people
    name: People partner
    hint: team health, inclusion, follow-up
parallel: [[facilitator, delivery], [people]]
limits: { rounds: 2, turns: 8, timeout: 2h }
brief: [context]
---
1. Tổng hợp chủ đề mà không quy lỗi cá nhân; phân biệt signal, anecdote và unknown.
2. Chọn tối đa vài action có owner, deadline và metric.
3. Gọi `workflow_done` với themes và actions.
