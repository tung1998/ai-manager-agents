---
key: review-pr
name: Review PR
description: Hai agent khác hãng model review cùng một thay đổi song song, rồi gộp thành một bản nhận xét có mức độ nghiêm trọng.
input: PR, nhánh hoặc phạm vi thay đổi cần review
inputs:
  - { key: context, description: "Phạm vi cần review (PR, nhánh, commit, file)", required: true }
  - { key: question, description: "Điều cần chú ý riêng" }
outputs:
  - { key: verdict, description: "approve | changes_requested", required: true }
  - { key: blocking, description: "Số vấn đề nghiêm trọng", type: number, required: true }
roles:
  - key: review-a
    name: Reviewer A
    hint: correctness, security
    access: analyze
    prefer: { tier: strong }
  - key: review-b
    name: Reviewer B
    hint: design, maintainability
    access: analyze
    differ_from: [review-a]
parallel: [[review-a, review-b]]
limits: { rounds: 2, turns: 6, timeout: 1h }
brief: [question, context]
---
1. Determine the scope to review (branch, PR, list of commits) and put it in `context`. `question`: review this change, list issues by severity (blocking, should fix, suggestion), each with file:line and the reason.
2. Call `workflow_delegate` for both `review-a` and `review-b` in the same turn. Write a short note, then end the turn.
3. Merge the two reviews: drop duplicates, keep the higher severity when they differ. For a point raised by only one side that you are unsure of, ask the other side (`workflow_ask`).
4. Call `workflow_done`: `summary` is the merged review; `outputs.verdict` (`changes_requested` when blocking issues remain), `outputs.blocking`.
