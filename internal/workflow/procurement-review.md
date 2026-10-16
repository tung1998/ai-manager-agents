---
key: procurement-review
name: Rà soát mua sắm
category: Finance & Legal
description: So sánh nhu cầu, nhà cung cấp, giá trị và điều khoản trước khi đưa yêu cầu mua sắm đi duyệt.
input: Yêu cầu mua sắm hoặc RFP
inputs:
  - { key: context, description: "Nhu cầu, tiêu chí, báo giá, ngân sách và timeline", required: true }
outputs:
  - { key: comparison, description: "So sánh lựa chọn và trade-off", required: true, type: list }
  - { key: recommendation, description: "Khuyến nghị và điều kiện phê duyệt", required: true }
roles:
  - { key: requester, name: Người yêu cầu, hint: outcome, acceptance, must-have }
  - { key: procurement, name: Mua sắm, hint: sourcing, negotiation, total cost, differ_from: [requester] }
  - { key: risk, name: Rủi ro, hint: security, legal, continuity }
parallel: [[requester, procurement], [risk]]
gates:
  - { key: purchase, name: Duyệt mua sắm, kind: approve, required: true }
limits: { rounds: 2, turns: 8, timeout: 3h }
brief: [context]
---
1. Chuẩn hóa requirement và so sánh total cost, lock-in, SLA, data và exit option.
2. Ghi rõ xung đột lợi ích hoặc evidence chưa đủ.
3. Mở gate `purchase`; sau khi duyệt gọi `workflow_done`.
