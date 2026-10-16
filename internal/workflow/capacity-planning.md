---
key: capacity-planning
name: Lập kế hoạch năng lực
category: Operations
description: Dự báo tải, tài nguyên và điểm nghẽn để chuẩn bị mở rộng có căn cứ.
input: Dịch vụ, team hoặc hạ tầng cần dự báo capacity
inputs:
  - { key: context, description: "Usage trend, baseline, limit, cost và kế hoạch tăng trưởng", required: true }
outputs:
  - { key: forecast, description: "Dự báo, ngưỡng cảnh báo và giả định", required: true, type: list }
  - { key: actions, description: "Việc mở rộng, tối ưu hoặc đo thêm", required: true, type: list }
roles:
  - { key: data, name: Phân tích tải, hint: trends, seasonality, uncertainty }
  - { key: platform, name: Hạ tầng, hint: limits, scaling, reliability, differ_from: [data] }
  - { key: finance, name: Chi phí, hint: budget, unit economics, trade-offs }
parallel: [[data, platform], [finance]]
limits: { rounds: 2, turns: 8, timeout: 3h }
brief: [context]
---
1. Kiểm tra baseline, seasonality, headroom, unit cost và chất lượng dữ liệu.
2. Lập nhiều kịch bản, trigger và hành động trước khi chạm ngưỡng.
3. Gọi `workflow_done` với forecast và actions.
