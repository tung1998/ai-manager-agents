[[if eq .Kind "tagged" -]]
[[.From]] tagged you in the chat. [[or .ReportBack "Answer the part meant for you in the latest message."]]
[[- else if eq .Kind "report" -]]
[[.From]] finished the task you handed over (see the latest message). [[or .ReportBack "Report the result to the person briefly and carry on if needed."]]
[[- else -]]
[[.From]] [[if .Role]]asks you, role [[.Role]][[else]]handed you a task[[end]]:
[[.Task]]

[[or .ReportBack "Do this part, then report the result briefly."]]
[[- end]]
