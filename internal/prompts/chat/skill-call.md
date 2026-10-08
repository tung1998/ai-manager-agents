The person invoked the skill /[[.Name]]. Follow the skill's instructions below for their request.
You only have read tools: if the skill says to run a command or edit a file, state the command to run or propose a diff instead of doing it yourself.

<skill name=[[printf "%q" .Name]] dir=[[printf "%q" .Dir]]>
[[.Body]]
</skill>
[[if .Others]]Other files in the skill folder (read when needed): [[join .Others ", "]]
[[end]]
Request: [[.Request]]
