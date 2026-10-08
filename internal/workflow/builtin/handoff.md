---
key: handoff
name: Giao lại
description: Giao trọn việc đang làm cho một agent khác, kèm bản bàn giao đủ bối cảnh. Dùng khi việc cần quyền hay chuyên môn khác, hoặc khi lập kế hoạch ở đây rồi để agent khác làm.
input: Việc cần giao (có thể kèm @tên agent nhận)
inputs:
  - { key: outcome, description: "Kết quả cần đạt", required: true }
  - { key: constraints, description: "Ràng buộc đã kiểm chứng" }
  - { key: done_when, description: "Tiêu chí xong" }
roles:
  - key: nguoi-nhan
    name: Người nhận
    hint: execution, changing code
    access: edit
limits: { rounds: 2, turns: 3, timeout: 2h, idle: 45m }
brief: [outcome, constraints, current_option, tried, done_when]
---
1. Read the calling chat's context (at the end of the first message) and the relevant code to know where the work stands.
2. Call `workflow_delegate` for the `nguoi-nhan` role. The recipient knows nothing about that chat, so the handoff must be self-contained:
   - `outcome`: the result the person needs, not how to do it;
   - `constraints`: verified constraints;
   - `current_option`: the approach being tried, stating clearly that the recipient may challenge it;
   - `tried`: what was tried and why it was dropped;
   - `done_when`: done criteria;
   - add `files` (paths only) and `must_not` if any.
   Do not go into which file or function to change: the recipient reads the code and decides.
3. Keep the person's intent exactly: investigate only means "do not change files"; a bug fix means "fix the bug"; a refactor means "refactor, do not rewrite".
4. Write a short note on who it was handed to, then end the turn. When the recipient is done, check the result against the done criteria; if not met, send the missing points (`workflow_send`).
5. Call `workflow_done`: `summary` is what the recipient did, the result against the done criteria, and remaining work (if any).
