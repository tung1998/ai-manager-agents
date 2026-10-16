---
key: budget-review
name: Rà soát ngân sách
category: Finance & Legal
description: Kiểm tra ngân sách, forecast và các khoản lệch để đề xuất điều chỉnh có căn cứ.
input: Ngân sách hoặc forecast cần review
inputs:
  - { key: context, description: "Budget, actuals, forecast, kỳ kế toán và mục tiêu", required: true }
outputs:
  - { key: variances, description: "Khoản lệch, nguyên nhân và mức độ", required: true, type: list }
  - { key: actions, description: "Điều chỉnh và câu hỏi cần owner trả lời", required: true, type: list }
roles:
  - { key: finance, name: Tài chính, hint: variance, forecast, accounting controls }
  - { key: owner, name: Budget owner, hint: plan, delivery, assumptions, differ_from: [finance] }
  - { key: leadership, name: Điều hành, hint: priorities, trade-offs, decisions }
parallel: [[finance, owner], [leadership]]
limits: { rounds: 2, turns: 8, timeout: 2h }
brief: [context]
---
1. Kiểm tra số liệu, kỳ, currency và baseline trước khi giải thích variance.
2. Phân biệt timing với run-rate, nêu assumption và impact nếu không điều chỉnh.
3. Gọi `workflow_done` với variances và actions.
