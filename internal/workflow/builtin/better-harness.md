---
key: better-harness
name: Khám vòng làm việc của agent
description: Như Better Harness (QoderAI) nhưng từ dữ liệu của office. Một script thu bằng chứng (diff gộp/bác, lệnh lỗi/bị từ chối, có chạy test trước khi gộp không, chỗ người dùng sửa agent, Burn, trí nhớ, file chỉ dẫn/skill/CI), ba agent đọc độc lập theo ba mảng, một agent chấm 5 mặt và nêu các việc nên sửa trước.
input: "Số ngày gần nhất cần xem (mặc định 30) và điều muốn chú ý, nếu có"
inputs:
  - { key: days, description: "Số ngày gần nhất (1-90)", type: number }
  - { key: focus, description: "Điều muốn chú ý (tùy chọn)" }
outputs:
  - { key: report, description: "Báo cáo: 5 mặt và các việc nên sửa", required: true }
roles:
  - key: phien
    name: Phiên làm việc
    hint: reading how the agents worked in chats (turns, commands, corrections, checks)
    access: analyze
    prefer: { tier: strong }
  - key: project
    name: Project
    hint: reading the project's own guides and checks (instructions, tests, CI, hooks, commands)
    access: analyze
    prefer: { tier: strong }
  - key: cong-cu
    name: Bộ công cụ
    hint: reading the agents' setup (office agents, skills, subagents, memories, Burn lessons)
    access: analyze
    prefer: { tier: strong }
  - key: tong-hop
    name: Tổng hợp
    hint: scoring the five parts of the agent work loop and ranking what to fix
    access: analyze
    prefer: { tier: strong }
limits: { timeout: 1h }
start: [thu]
steps:
  - id: thu
    type: code
    name: Thu bằng chứng
    lang: bash
    timeout_s: 300
    script: |
      "${OFFICE_BIN:-office}" harness "$OFFICE_PROJECT" --days "${OFFICE_INPUT_DAYS:-30}"
    next: [phien, project, cong-cu]
    position: { x: 0, y: 0 }
  - id: phien
    type: agent
    name: Phiên làm việc
    role: phien
    prompt: |
      You are one of three independent, read-only evidence passes of a harness review (the loop around the coding agents, not the code itself). Do not change anything.
      The office collected this evidence bundle (JSON) for the project over the last days:
      {{steps.thu.output}}

      Focus asked by the person: {{input.focus}}

      Your lane: the SESSIONS. How did the agents work: diffs merged, refused by the person or failing to merge (diffs), commands failed or refused (commands), failed turns (turns), whether a check ran before each merged diff (validation), and where the person corrected an agent (corrections: messages that look like corrections, some are only questions). Find the patterns behind them: wrong goal, skipped checks, unsafe commands, the same mistake again.
      Chat ids in the bundle open with read_link at /projects/<project_id>?tab=chat&c=<chat id>. Open a few where they decide a point; do not read everything.
      Missing data is "unobserved", never a guess.

      Answer: 3-6 patterns, each with its numbers and 1-2 chat ids as evidence, and what in the harness would have prevented it.
    next: tong-hop
    on_error: continue
    position: { x: 320, y: -200 }
  - id: project
    type: agent
    name: Project
    role: project
    prompt: |
      You are one of three independent, read-only evidence passes of a harness review (the loop around the coding agents, not the code itself). Do not change anything.
      The office collected this evidence bundle (JSON) for the project over the last days:
      {{steps.thu.output}}

      Focus asked by the person: {{input.focus}}

      Your lane: the PROJECT HARNESS. Read the repo: instruction files (CLAUDE.md, AGENTS.md, rules, README for agents), how to build/test/lint and whether that is written down, CI, hooks, the project's commands in office (assets.processes), the project description. Answer: does an agent landing here know the goal, the conventions and what "done" means, and is there a fast, documented check it can run? Compare with what the bundle shows agents actually ran (validation.check_commands).
      Cite file:line. Missing data is "unobserved", never a guess. A file existing does not prove it is followed.

      Answer: what exists, gaps with evidence, 3-5 concrete improvements (what file, what to write, how to verify).
    next: tong-hop
    on_error: continue
    position: { x: 320, y: 0 }
  - id: cong-cu
    type: agent
    name: Bộ công cụ
    role: cong-cu
    prompt: |
      You are one of three independent, read-only evidence passes of a harness review (the loop around the coding agents, not the code itself). Do not change anything.
      The office collected this evidence bundle (JSON) for the project over the last days:
      {{steps.thu.output}}

      Focus asked by the person: {{input.focus}}

      Your lane: the AGENTS' SETUP and LEARNING. Look at the office agents (assets.agents: their instructions, roles; describe/list/get show them), skills and subagents in the repo, the agents' memories (learning: count, topics, new in the window), Burn's lessons and its failed or refused work (burn), and repeated failures (learning.repeated_errors). Answer: are the agents' roles and instructions clear and not overlapping, does a lesson from one task reach the next, and does the same failure keep coming back without a rule or a skill to stop it?
      Missing data is "unobserved", never a guess. A count of skills or memories only routes the inspection, it is not a score.

      Answer: what exists, gaps with evidence, 3-5 concrete improvements (what to add or change, how to verify).
    next: tong-hop
    on_error: continue
    position: { x: 320, y: 200 }
  - id: tong-hop
    type: agent
    name: Tổng hợp
    role: tong-hop
    prompt: |
      You lead a harness review: three independent passes read the evidence below. Combine them; do not change anything.

      Evidence bundle:
      {{steps.thu.output}}

      Pass 1, sessions:
      {{steps.phien.output}}

      Pass 2, project harness:
      {{steps.project.output}}

      Pass 3, agents' setup and learning:
      {{steps.cong-cu.output}}

      Focus asked by the person: {{input.focus}}
      If a pass only holds an error, say it did not answer and work from the others.

      Write the report for the person, in their language, short and plain:
      1. The five parts of the agent work loop, one line each with a status (good / to watch / weak / unobserved) and the numbers behind it:
         - Task understanding: do the agents know the goal and what "done" means?
         - Controlled execution: is the work done on known, repeatable paths?
         - Change validation: is there proof a change works before it is merged?
         - Reliable delivery: do reviews, approvals and git steps hold?
         - Learning capture: does the next task benefit from this one?
      2. Up to 5 findings, most important first. Each: the evidence (numbers, chat ids, file:line), why it matters, the scoped fix (which file or setting, what to write), and how to check it worked next time (which number in this review should move).
      3. What is unobserved and how to observe it.
      Never turn a missing signal into a score, and keep counts of assets apart from proof that they work.
    next: xong
    position: { x: 640, y: 0 }
  - id: xong
    type: end
    summary: "{{steps.tong-hop.output}}"
    outputs:
      report: "{{steps.tong-hop.output}}"
    position: { x: 960, y: 0 }
---
The workflow runs by the steps in the header (steps).
