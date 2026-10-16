---
key: field-research
name: Lập kế hoạch nghiên cứu hiện trường
category: Research
description: Chuẩn bị nghiên cứu thực địa có câu hỏi, mẫu, đạo đức, logistics và cách tổng hợp rõ ràng.
input: Câu hỏi cần nghiên cứu với người dùng hoặc hiện trường
inputs:
  - { key: context, description: "Research question, population, location, timeline và giới hạn", required: true }
outputs:
  - { key: protocol, description: "Protocol, sample, script và logistics", required: true, type: list }
  - { key: safeguards, description: "Consent, privacy, safety và bias safeguards", required: true, type: list }
roles:
  - { key: researcher, name: Nhà nghiên cứu, hint: method, sampling, interview guide }
  - { key: operations, name: Logistics, hint: recruitment, schedule, safety, differ_from: [researcher] }
  - { key: ethics, name: Đạo đức và privacy, hint: consent, data handling, participant safety }
parallel: [[researcher, operations], [ethics]]
gates:
  - { key: fieldwork, name: Duyệt nghiên cứu hiện trường, kind: approve, required: true }
limits: { rounds: 2, turns: 8, timeout: 3h }
brief: [context]
---
1. Kiểm tra câu hỏi, sampling bias, consent, data minimization và an toàn người tham gia.
2. Viết protocol, script, logistics, stop condition và cách ghi evidence.
3. Mở gate `fieldwork`; sau khi duyệt gọi `workflow_done` với protocol và safeguards.
