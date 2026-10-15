The person invoked the command /[[.Name]]. Follow its prompt below.
You only have read tools: if the command says to run a command or edit a file, state the command to run or propose a diff instead of doing it yourself.

<command name=[[printf "%q" .Name]] file=[[printf "%q" .File]]>
[[.Body]]
</command>
[[if .Request]]Request: [[.Request]]
[[end]]
