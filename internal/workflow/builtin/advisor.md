---
key: advisor
name: Cố vấn
description: Hỏi ý kiến thứ hai từ một agent dùng model khác hãng, không giao việc cho nó. Dùng khi phân vân giữa các hướng, muốn soát lại một quyết định hay một thay đổi rủi ro.
input: Câu hỏi cần cố vấn
inputs:
  - { key: question, description: "Câu hỏi cần cố vấn", required: true }
  - { key: context, description: "Bối cảnh và những hướng đã xét" }
outputs:
  - { key: recommendation, description: "Khuyến nghị cuối cùng kèm lý do", required: true }
roles:
  - key: co-van
    name: Cố vấn
    hint: design, review, risk analysis
    access: analyze
    differ_from: [dieu-phoi]
    prefer: { tier: strong }
limits: { rounds: 3, turns: 4, timeout: 1h }
brief: [question, context, tried]
---
1. From the request and context of the calling chat, write a sharp question: what needs deciding, enough context to understand it, the options considered and rejected with reasons; give file paths only, do not paste contents. Ask the advisor for a **recommendation with reasons** and what would prove that recommendation wrong.
2. Ask `co-van` with `workflow_ask` (waits for the reply). If it times out, the advisor keeps working in the background and you are called back when it is done.
3. If points still need clarifying, ask again with `workflow_ask` (the advisor keeps the thread; each ask is one round).
4. Call `workflow_done`: `summary` holds the advisor's opinion and your recommendation (agree or not, and why); `outputs.recommendation` is the final recommendation.
