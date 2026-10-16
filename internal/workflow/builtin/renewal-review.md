---
key: renewal-review
name: Rà soát gia hạn khách hàng
category: Customer
description: Đánh giá sức khỏe tài khoản, giá trị đã đạt và rủi ro gia hạn để lập kế hoạch giữ khách hàng.
input: Tài khoản sắp gia hạn
inputs:
  - { key: context, description: "Usage, ticket, renewal date, contract và health score", required: true }
outputs:
  - { key: health, description: "Tín hiệu tích cực, rủi ro và bằng chứng", required: true, type: list }
  - { key: plan, description: "Kế hoạch gia hạn có owner và mốc", required: true, type: list }
roles:
  - key: success
    name: Customer success
    hint: adoption, outcomes, champions
  - key: commercial
    name: Thương mại
    hint: contract, pricing, forecast
    differ_from: [success]
  - key: product
    name: Sản phẩm
    hint: gaps, roadmap, feedback
parallel: [[success, commercial], [product]]
limits: { rounds: 2, turns: 8, timeout: 2h }
brief: [context]
---
1. Đối chiếu usage với outcome khách hàng và contract; ghi rõ dữ liệu thiếu.
2. Xếp hạng risk, đề xuất value conversation và mốc hành động.
3. Gọi `workflow_done` với health và plan.
