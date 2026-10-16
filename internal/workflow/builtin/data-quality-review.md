---
key: data-quality-review
name: Kiểm tra chất lượng dữ liệu
description: Xác định phạm vi, đo chất lượng, tìm nguyên nhân và lập kế hoạch làm sạch dữ liệu có kiểm soát.
input: Dataset, pipeline hoặc báo cáo có dấu hiệu dữ liệu sai
inputs:
  - { key: context, description: "Bảng/nguồn dữ liệu, owner, kỳ dữ liệu và triệu chứng", required: true }
  - { key: constraints, description: "SLA, quy định dữ liệu và thời gian cho phép gián đoạn" }
outputs:
  - { key: findings, description: "Lỗi theo rule, mức độ, tỷ lệ và bằng chứng", required: true, type: list }
  - { key: remediation, description: "Kế hoạch sửa, kiểm chứng và phòng tái diễn", required: true, type: list }
roles:
  - key: analyst
    name: Phân tích dữ liệu
    hint: profiling, distributions, anomaly detection
  - key: owner
    name: Chủ nguồn dữ liệu
    hint: business meaning, lineage, upstream changes
    differ_from: [analyst]
  - key: platform
    name: Nền tảng dữ liệu
    hint: pipeline, contracts, tests, monitoring
    access: propose
parallel: [[analyst, owner], [platform]]
gates:
  - key: production-change
    name: Duyệt thay đổi dữ liệu production
    kind: approve
    required: true
limits: { rounds: 3, turns: 10, timeout: 3h, idle: 45m }
brief: [constraints, context]
---
1. Giao `analyst` profile dữ liệu và đo completeness, freshness, validity, uniqueness, consistency; không sửa dữ liệu.
2. Giao `owner` xác nhận ý nghĩa nghiệp vụ, lineage và thay đổi gần đây; tách lỗi thật khỏi ngoại lệ hợp lệ.
3. Giao `platform` tìm điểm hỏng upstream, đề xuất data contract, test và monitoring.
4. Hợp nhất finding theo impact và khả năng tái diễn, kèm sample và rule có thể kiểm chứng.
5. Mở gate `production-change` trước khi làm sạch hoặc backfill; gọi `workflow_done` với finding và remediation.
