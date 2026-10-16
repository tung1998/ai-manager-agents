---
key: security-assessment
name: Đánh giá bảo mật
description: Rà threat model, quyền, dữ liệu và đường tấn công trước khi một tính năng hoặc tích hợp được đưa vào sử dụng.
input: Tính năng, thay đổi kiến trúc hoặc tích hợp cần đánh giá
inputs:
  - { key: context, description: "Phạm vi, data flow, trust boundary và tài liệu liên quan", required: true }
  - { key: constraints, description: "Chuẩn, policy hoặc yêu cầu compliance" }
outputs:
  - { key: findings, description: "Finding theo mức độ, bằng chứng và cách giảm thiểu", required: true, type: list }
  - { key: verdict, description: "go | go_with_changes | no_go", required: true }
roles:
  - key: threat
    name: Threat model
    hint: assets, actors, trust boundaries, abuse cases
    prefer: { tier: strong }
  - key: implementation
    name: Kiểm tra triển khai
    hint: auth, secrets, dependencies, logging
    access: analyze
    differ_from: [threat]
  - key: operations
    name: Phòng thủ vận hành
    hint: monitoring, incident response, recovery, least privilege
    access: propose
parallel: [[threat, implementation], [operations]]
gates:
  - key: security-signoff
    name: Người phụ trách duyệt rủi ro còn lại
    kind: approve
    required: true
limits: { rounds: 3, turns: 10, timeout: 4h, idle: 45m }
brief: [constraints, context]
---
1. Giao ba vai trò rà độc lập assets, threat, implementation và khả năng phát hiện/phục hồi; cấm thử exploit trên production.
2. Hợp nhất finding theo impact và likelihood, loại duplicate nhưng giữ bằng chứng và owner.
3. Nếu còn finding high/critical hoặc thiếu bằng chứng, nêu rõ blocker. Mở gate `security-signoff` với residual risk và phương án giảm thiểu.
4. Gọi `workflow_done` với `outputs.findings`, verdict và điều kiện cần đạt trước khi triển khai.
