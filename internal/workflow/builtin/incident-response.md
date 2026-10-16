---
key: incident-response
name: Phản ứng sự cố
description: Phân loại impact, điều tra song song, giữ liên lạc và đưa hệ thống về trạng thái an toàn với checkpoint của con người.
input: Sự cố, cảnh báo hoặc suy giảm dịch vụ cần điều phối
inputs:
  - { key: context, description: "Triệu chứng, thời điểm, service và bằng chứng ban đầu", required: true }
  - { key: severity, description: "Mức độ hoặc SLA đang áp dụng" }
outputs:
  - { key: incident_summary, description: "Impact, timeline, nguyên nhân hiện biết và trạng thái phục hồi", required: true }
  - { key: next_actions, description: "Việc tiếp theo có owner và hạn", required: true, type: list }
roles:
  - key: triage
    name: Phân loại
    hint: impact, scope, severity, timeline
    prefer: { tier: strong }
  - key: technical
    name: Kỹ thuật
    hint: logs, metrics, dependencies, recovery
    access: propose
  - key: communications
    name: Liên lạc
    hint: stakeholder updates, status page, customer impact
    access: propose
    differ_from: [technical]
parallel: [[triage, technical], [communications]]
gates:
  - key: recovery
    name: Duyệt bước phục hồi có tác động
    kind: approve
    required: true
limits: { rounds: 3, turns: 10, timeout: 3h, idle: 30m }
brief: [context]
---
1. Giao `triage` xác định impact, phạm vi, severity và timeline từ bằng chứng; không sửa gì khi chưa rõ blast radius.
2. Giao `technical` điều tra log/metric/trace và đề xuất containment hoặc recovery có rollback.
3. Giao `communications` soạn cập nhật ngắn, đúng mức chắc chắn, không đổ lỗi và không lộ dữ liệu.
4. Hợp nhất các kết quả, mở gate `recovery` trước hành động có tác động; nếu bị từ chối, đề xuất phương án ít rủi ro hơn.
5. Sau khi xác nhận phục hồi, gọi `workflow_done` với timeline, nguyên nhân hiện biết, bằng chứng và `outputs.next_actions`.
