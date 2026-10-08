---
key: council
name: Hội đồng
description: Hai agent khác hãng model phân tích độc lập rồi thống nhất một kế hoạch. Dùng khi bế tắc, sửa mãi không xong, hay bài toán khó cần lùi lại tìm nguyên nhân gốc.
input: Vấn đề cần phân tích
inputs:
  - { key: question, description: "Vấn đề cần phân tích (điều gì sai, kết quả cần đạt)", required: true }
  - { key: context, description: "Đã thử gì, vì sao chưa được" }
outputs:
  - { key: plan, description: "Kế hoạch hai bên thống nhất", required: true }
  - { key: agreed, description: "Hai bên có thống nhất không", type: boolean, required: true }
roles:
  - key: a
    name: Thành viên A
    hint: planning, root cause analysis
    access: analyze
    prefer: { tier: strong }
  - key: b
    name: Thành viên B
    hint: critique, finding risks
    access: analyze
    differ_from: [a]
    prefer: { tier: strong }
parallel: [[a, b]]
limits: { rounds: 3, turns: 8, timeout: 1h }
brief: [question, context]
---
1. Write the brief at the problem level (`question`): what is wrong, what outcome is needed. Do not propose a solution. `context` covers what was tried and why it did not work (from the calling chat's context); `files` are paths.
2. Call `workflow_delegate` for both `a` and `b` in the same turn, with the same brief. Write a short note, then end the turn.
3. With both results in: compare. Where they disagree, send one side's reasoning to the other with `workflow_send` and ask whether they change their mind.
4. Stop when both agree or the rounds run out. Call `workflow_done`: `summary` is the shared conclusion, the proposed plan, where they disagreed and how it was resolved; `outputs.plan`, `outputs.agreed`.
