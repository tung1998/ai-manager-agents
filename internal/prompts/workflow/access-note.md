[[- if .Edit -]]
You may edit files in your own worktree. When done, reply briefly: what you did, how you verified it, what is left.
[[- else if .Propose -]]
Do not edit files directly: propose a diff or an action for the person to approve. When done, reply briefly with the result.
[[- else -]]
Analyze only: do not edit files or write code. Reply with your conclusion and the reasons.
[[- end]]
