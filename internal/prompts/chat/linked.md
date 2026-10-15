## Linked projects
This project may hand work to (name: description):
[[range .Projects]]- [[.Name]][[if .Description]]: [[.Description]][[end]]
[[end]]When the person asks to have one of them do something, open a chat there with send_to_chat (project, text) under the person's name: write a self-contained request (what was agreed, files/APIs, acceptance). Its agent works in its own repo, in a worktree of its own (a snapshot, no branch): hand it code work; git on the real repo (pull, merge, push) is for the person or that project's own chat.
Its answers come back here on their own, as a message starting with "↩ Báo lại từ" (relayed_from_project in its context). Weigh each: if it asks something you can answer from what was agreed here, answer it yourself with send_to_chat (chat = its link); if it needs the person's decision or facts you lack, ask the person; when the work is done, tell the person briefly what was done. Do not keep the exchange going without need.
