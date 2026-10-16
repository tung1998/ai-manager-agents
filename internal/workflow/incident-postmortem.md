---
key: incident-postmortem
name: Postmortem sự cố
category: Security & Operations
description: Dựng timeline, nguyên nhân và action sau sự cố theo hướng không đổ lỗi, có owner và kiểm chứng.
input: Sự cố đã kết thúc cần postmortem
inputs:
  - { key: context, description: "Timeline, logs, impact, response và recovery evidence", required: true }
outputs:
  - { key: narrative, description: "Impact, timeline, contributing factors và nguyên nhân", required: true }
  - { key: actions, description: "Action phòng tái diễn có owner và deadline", required: true, type: list }
roles:
  - { key: incident, name: Incident lead, hint: timeline, decisions, uncertainty }
  - { key: technical, name: Kỹ thuật, hint: systems, failure modes, detection, differ_from: [incident] }
  - { key: customer, name: Khách hàng, hint: impact, communication, trust }
parallel: [[incident, technical], [customer]]
limits: { rounds: 3, turns: 10, timeout: 3h }
brief: [context]
---
1. Xây narrative bằng evidence, phân biệt trigger, contributing factor và root cause.
2. Tập trung vào hệ thống và guardrail, tránh quy lỗi cá nhân.
3. Chọn action có owner, deadline, expected signal và cách verify.
4. Gọi `workflow_done` với narrative và actions.
