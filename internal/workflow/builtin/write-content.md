---
key: write-content
name: Viết nội dung
description: Viết bài (blog, Facebook, email cho khách), có agent khác hãng model phản biện, người dùng duyệt trước khi đăng hay gửi.
input: Chủ đề, đối tượng đọc, kênh đăng
inputs:
  - { key: outcome, description: "Mục đích của bài và người đọc", required: true }
  - { key: constraints, description: "Kênh, độ dài, giọng văn, điều không được nói" }
outputs:
  - { key: final, description: "Bản cuối đã được duyệt", required: true }
  - { key: published, description: "Đã đăng hay gửi chưa", type: boolean, required: true }
roles:
  - key: viet
    name: Người viết
    hint: writing content
    access: analyze
  - key: phan-bien
    name: Phản biện
    hint: editing, fact-checking
    access: analyze
    differ_from: [viet]
gates:
  - key: duyet
    name: Người dùng duyệt bản cuối
    kind: approve
    required: true
limits: { rounds: 3, turns: 8, timeout: 2h }
brief: [outcome, constraints]
---
1. Assign `viet` to write a draft: `outcome` is the piece's purpose and audience; `constraints` are the channel, length, tone, and what must not be said.
2. Assign `phan-bien` to check the draft: wrong facts, unclear parts, off-tone parts. Send the feedback back to `viet` (`workflow_send`) until it is good.
3. Open the `duyet` gate with `workflow_gate` and the final version, then end the turn and wait for the person to approve. If rejected, revise per the reason or stop.
4. Only once approved, publish or send (via the project's tools, still within your permissions). Call `workflow_done`: `summary` is the final version and where it was published/sent; `outputs.final`, `outputs.published`.
