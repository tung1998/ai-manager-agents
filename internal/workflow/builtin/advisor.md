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
    hint: thiết kế, review, phân tích rủi ro
    access: analyze
    differ_from: [dieu-phoi]
    prefer: { tier: strong }
limits: { rounds: 3, turns: 4, timeout: 1h }
brief: [question, context, tried]
---
1. Từ yêu cầu và bối cảnh của cuộc chat đã gọi, viết câu hỏi thật sắc: điều cần quyết, bối cảnh đủ để hiểu, những hướng đã xét và đã loại kèm lý do; file chỉ ghi đường dẫn, không dán nội dung. Yêu cầu cố vấn đưa **khuyến nghị kèm lý do** và điều sẽ chứng minh khuyến nghị đó sai.
2. Hỏi `co-van` bằng `workflow_ask` (chờ trả lời ngay). Quá hạn thì cố vấn làm tiếp ở nền và bạn được gọi lại khi xong.
3. Còn điểm cần làm rõ thì hỏi tiếp bằng `workflow_ask` (cố vấn giữ mạch; mỗi lần là một vòng).
4. Gọi `workflow_done`: `summary` gồm ý kiến của cố vấn và khuyến nghị của bạn (đồng ý hay không, vì sao); `outputs.recommendation` là khuyến nghị cuối.
