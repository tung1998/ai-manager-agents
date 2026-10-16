---
key: sales-deal-review
name: Rà soát cơ hội bán hàng
description: Kiểm tra nhu cầu, người quyết định, cạnh tranh, rủi ro hợp đồng và bước tiếp theo trước khi chốt một cơ hội.
input: Cơ hội bán hàng cần đánh giá
inputs:
  - { key: context, description: "Account, nhu cầu, stage, deal size, timeline và ghi chú hiện có", required: true }
  - { key: constraints, description: "Mục tiêu doanh thu, policy giá hoặc điều khoản bắt buộc" }
outputs:
  - { key: scorecard, description: "Các tiêu chí đã kiểm và bằng chứng", required: true, type: list }
  - { key: next_step, description: "Bước tiếp theo, owner và điều kiện để tiến stage", required: true }
roles:
  - key: account
    name: Account owner
    hint: customer context, relationship, next meeting
  - key: solution
    name: Tư vấn giải pháp
    hint: fit, implementation, technical risk
    differ_from: [account]
  - key: commercial
    name: Thương mại
    hint: pricing, procurement, legal, forecast risk
parallel: [[account, solution], [commercial]]
gates:
  - key: commit
    name: Người phụ trách duyệt cam kết cơ hội
    kind: approve
    required: true
limits: { rounds: 2, turns: 8, timeout: 2h, idle: 30m }
brief: [constraints, context]
---
1. Giao ba vai trò đánh giá độc lập: nhu cầu và champion, độ phù hợp giải pháp, rủi ro thương mại và forecast.
2. Phân biệt bằng chứng đã xác nhận với suy đoán; nêu rõ thông tin còn thiếu và cách lấy thông tin đó.
3. Hợp nhất scorecard, đối thủ, rủi ro triển khai, điều kiện thắng và bước tiếp theo có owner.
4. Mở gate `commit` với khuyến nghị commit, nurture hoặc loại khỏi forecast; chờ người phụ trách duyệt.
5. Gọi `workflow_done` với `outputs.scorecard` và `outputs.next_step`.
