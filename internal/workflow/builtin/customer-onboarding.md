---
key: customer-onboarding
name: Onboarding khách hàng
description: Lập kế hoạch đưa khách hàng mới vào sử dụng, phối hợp triển khai, đào tạo và chăm sóc với mốc bàn giao rõ ràng.
input: Khách hàng mới, gói dịch vụ và mục tiêu triển khai
inputs:
  - { key: context, description: "Thông tin khách hàng, sản phẩm, người liên hệ và mốc thời gian", required: true }
  - { key: outcome, description: "Kết quả cần đạt trong giai đoạn onboarding" }
outputs:
  - { key: plan, description: "Kế hoạch theo mốc, owner, dependency và rủi ro", required: true, type: list }
  - { key: handoff, description: "Điều kiện bàn giao sang chăm sóc thường xuyên", required: true }
roles:
  - key: success
    name: Customer success
    hint: goals, adoption, stakeholder alignment
  - key: implementation
    name: Triển khai
    hint: setup, integration, migration, dependencies
    access: propose
    differ_from: [success]
  - key: enablement
    name: Đào tạo và tài liệu
    hint: training, documentation, user readiness
parallel: [[success, implementation], [enablement]]
limits: { rounds: 3, turns: 9, timeout: 3h, idle: 45m }
brief: [outcome, context]
---
1. Giao `success` xác định mục tiêu, nhóm người dùng, tiêu chí adoption và các bên liên quan; đánh dấu giả định chưa xác nhận.
2. Giao `implementation` lập danh sách setup, tích hợp, dữ liệu, dependency và phương án rollback; không tự ý thay đổi hệ thống khách hàng.
3. Giao `enablement` đề xuất lộ trình đào tạo, tài liệu, FAQ và cách đo mức sẵn sàng.
4. Hợp nhất thành kế hoạch có mốc, owner, dependency, rủi ro và điều kiện escalations.
5. Gọi `workflow_done` với `outputs.plan`, `outputs.handoff` và các câu hỏi cần khách hàng xác nhận.
