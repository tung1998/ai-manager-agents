---
key: review-pr
name: Review PR
description: Hai agent khác hãng model review cùng một thay đổi song song, rồi gộp thành một bản nhận xét có mức độ nghiêm trọng.
input: PR, nhánh hoặc phạm vi thay đổi cần review
roles:
  - key: review-a
    name: Reviewer A
    hint: đúng sai, bảo mật
    access: analyze
  - key: review-b
    name: Reviewer B
    hint: thiết kế, dễ bảo trì
    access: analyze
    differ_from: [review-a]
parallel: [[review-a, review-b]]
limits: { rounds: 2, turns: 6, timeout: 1h }
brief: [question, context]
---
1. Xác định phạm vi cần review (nhánh, PR, danh sách commit) và đưa vào `context`. `question`: review thay đổi này, liệt kê vấn đề theo mức độ (nghiêm trọng, nên sửa, góp ý), mỗi vấn đề kèm file:dòng và lý do.
2. Gọi `workflow_delegate` cho cả `review-a` và `review-b` trong cùng một lượt.
3. Gộp hai bản: bỏ trùng, giữ mức nghiêm trọng cao hơn khi hai bên khác nhau. Điểm chỉ một bên nêu mà bạn không chắc thì hỏi lại bên kia (`workflow_send`).
4. Báo người dùng bản review gộp. Gọi `workflow_done`.
