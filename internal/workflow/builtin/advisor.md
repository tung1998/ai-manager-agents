---
key: advisor
name: Cố vấn
description: Hỏi ý kiến thứ hai từ một agent dùng model khác hãng, không giao việc cho nó. Dùng khi phân vân giữa các hướng, muốn soát lại một quyết định hay một thay đổi rủi ro.
input: Câu hỏi cần cố vấn
roles:
  - key: co-van
    name: Cố vấn
    hint: thiết kế, review, phân tích rủi ro
    access: analyze
    differ_from: [dieu-phoi]
limits: { rounds: 3, turns: 4, timeout: 1h }
brief: [question, context, tried]
---
1. Viết câu hỏi thật sắc (`question`), bối cảnh đủ để hiểu (`context`), những hướng đã xét và đã loại kèm lý do (`tried`); thêm `files` là đường dẫn, không dán nội dung. Yêu cầu cố vấn đưa **khuyến nghị kèm lý do**.
2. Gọi `workflow_delegate` cho vai `co-van`, báo người dùng đang hỏi ý kiến rồi dừng lượt.
3. Có câu trả lời: viết cho người dùng hai phần, ý kiến của cố vấn và khuyến nghị của bạn (đồng ý hay không, vì sao).
4. Còn điểm cần làm rõ thì hỏi tiếp bằng `workflow_send` (cố vấn giữ mạch). Xong thì gọi `workflow_done`.
