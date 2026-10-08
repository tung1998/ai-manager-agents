---
key: council-3
name: Hội đồng 3 bên
description: Tam quyền phân lập. Lập kế hoạch đặt mục tiêu và kế hoạch, Thực thi làm theo kế hoạch đã thông qua, Giám sát kiểm tra và có quyền phủ quyết. Mọi quyết định cần 2/3 đồng ý.
input: Mục tiêu cần làm
inputs:
  - { key: outcome, description: "Mục tiêu cần đạt", required: true }
  - { key: constraints, description: "Ràng buộc đã kiểm chứng" }
  - { key: done_when, description: "Tiêu chí xong" }
outputs:
  - { key: accepted, description: "Kết quả có được nghiệm thu không", type: boolean, required: true }
roles:
  - key: lap-ke-hoach
    name: Lập kế hoạch
    hint: legislative; goals, rules, plans
    access: analyze
    prefer: { tier: strong }
  - key: thuc-thi
    name: Thực thi
    hint: executive; doing the work, changing code
    access: edit
  - key: giam-sat
    name: Giám sát
    hint: judicial; checking evidence, veto
    access: analyze
    differ_from: [thuc-thi]
    prefer: { tier: strong }
vote:
  roles: [lap-ke-hoach, thuc-thi, giam-sat]
  quorum: 2
  veto: [giam-sat]
limits: { rounds: 4, turns: 16, timeout: 3h, idle: 45m }
brief: [outcome, constraints, done_when]
---
1. Assign `lap-ke-hoach` to write the plan: goal, scope, done criteria, risks. The plan states outcomes and constraints, not a ready-made fix for each file.
2. Put the plan to a vote with `workflow_vote`. If it does not pass, send it back to `lap-ke-hoach` (`workflow_send`) with the objections, at most 2 times; if it still does not pass, call `workflow_done` stating clearly why it did not pass (`accepted: false`).
3. Once it passes, assign `thuc-thi` to carry out exactly that plan.
4. Assign `giam-sat` to check the result against the plan and done criteria, based on evidence (diff, test results), not on claims.
5. Put the result to an acceptance vote. If vetoed or short of votes, send `thuc-thi` the points to fix.
6. Call `workflow_done`: `summary` is the approved plan, what was done, the vote results; `outputs.accepted`.
