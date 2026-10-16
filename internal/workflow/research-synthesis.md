---
key: research-synthesis
name: Tổng hợp nghiên cứu
category: Research
description: Gộp nhiều nguồn nghiên cứu thành kết luận có trích dẫn, mức chắc chắn và câu hỏi còn mở.
input: Câu hỏi nghiên cứu và tập nguồn
inputs:
  - { key: context, description: "Câu hỏi, nguồn, ngày, phương pháp và giới hạn", required: true }
outputs:
  - { key: synthesis, description: "Kết luận theo chủ đề và citation", required: true, type: list }
  - { key: open_questions, description: "Điểm chưa đủ evidence và bước tiếp theo", required: true, type: list }
roles:
  - { key: collector, name: Thu thập nguồn, hint: source quality, recency, coverage }
  - { key: analyst, name: Phân tích, hint: themes, contradictions, uncertainty, differ_from: [collector] }
  - { key: reviewer, name: Phản biện, hint: bias, unsupported claims, citations }
parallel: [[collector, analyst], [reviewer]]
limits: { rounds: 3, turns: 9, timeout: 3h }
brief: [context]
---
1. Ghi nguồn và ngày cho từng claim; phân biệt fact, interpretation và speculation.
2. Tìm bằng chứng phản bác, nguồn phụ thuộc lẫn nhau và khoảng trống.
3. Gọi `workflow_done` với synthesis và open_questions.
