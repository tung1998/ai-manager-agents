---
key: experiment-review
name: Đánh giá experiment
category: Product
description: Đánh giá kết quả A/B test hoặc pilot, kiểm tra chất lượng bằng chứng và quyết định bước tiếp theo.
input: Experiment hoặc pilot đã có kết quả
inputs:
  - { key: context, description: "Hypothesis, design, sample, metrics, result và thời gian chạy", required: true }
  - { key: outcome, description: "Quyết định cần đưa ra" }
outputs:
  - { key: conclusion, description: "Kết luận có giới hạn và confidence", required: true }
  - { key: next_step, description: "Ship, iterate, stop hoặc chạy thêm", required: true }
roles:
  - { key: analyst, name: Phân tích, hint: "metrics, bias, significance" }
  - { key: product, name: Sản phẩm, hint: "hypothesis, user value, decision", differ_from: [analyst] }
  - { key: engineering, name: Kỹ thuật, hint: "instrumentation, reliability, rollout" }
parallel: [[analyst, product], [engineering]]
limits: { rounds: 2, turns: 8, timeout: 2h }
brief: [outcome, context]
---
1. Kiểm tra guardrail, sample, instrumentation và các yếu tố làm sai lệch kết quả.
2. Phân biệt kết quả thống kê với ý nghĩa sản phẩm; nêu uncertainty.
3. Gọi `workflow_done` với conclusion và next_step.
