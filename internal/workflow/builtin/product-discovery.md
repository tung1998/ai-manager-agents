---
key: product-discovery
name: Khám phá sản phẩm
description: Kết hợp nhu cầu khách hàng, dữ liệu sử dụng và tính khả thi kỹ thuật để chọn vấn đề đáng giải quyết trước khi build.
input: Cơ hội, phản hồi hoặc vấn đề sản phẩm cần làm rõ
inputs:
  - { key: outcome, description: "Kết quả kinh doanh/trải nghiệm mong muốn", required: true }
  - { key: context, description: "Feedback, dữ liệu usage và ràng buộc hiện có" }
outputs:
  - { key: recommendation, description: "Vấn đề, người dùng, lựa chọn và khuyến nghị", required: true }
  - { key: evidence, description: "Bằng chứng và giả định cần kiểm chứng", required: true, type: list }
roles:
  - key: customer
    name: Tiếng nói khách hàng
    hint: jobs, pain points, segments, demand signals
    prefer: { tier: strong }
  - key: data
    name: Phân tích dữ liệu
    hint: usage, funnel, experiment, metrics
    differ_from: [customer]
  - key: feasibility
    name: Tính khả thi
    hint: architecture, effort, dependencies, risks
    prefer: { tier: balanced }
parallel: [[customer, data]]
limits: { rounds: 3, turns: 9, timeout: 3h, idle: 45m }
brief: [outcome, context]
---
1. Giao `customer` và `data` làm rõ nhu cầu và tín hiệu độc lập; mỗi kết luận phải có nguồn hoặc gắn nhãn giả định.
2. Giao `feasibility` đánh giá các hướng, dependency, chi phí cơ hội và rủi ro triển khai.
3. Loại bỏ ý tưởng chỉ có tín hiệu yếu; nêu lựa chọn không làm gì hoặc thử nghiệm nhỏ nếu phù hợp.
4. Gọi `workflow_done` với problem statement, recommendation, evidence, giả định và cách kiểm chứng tiếp theo.
