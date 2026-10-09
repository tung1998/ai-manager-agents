Starter packs (valid pack_key values):
[[.Packs]]

[[if .Commands]]Project's command catalog (only pick "commands" from this list):
[[.Commands]]

[[end]][[if .Skills]]Skills in the office library (only pick "skills" from this list, by name):
[[.Skills]]

[[end]][[if .MCP]]MCP servers office can add (only pick "mcp" from this list, by name; "needs": key = a person must paste a key, login = sign in on first use):
[[.MCP]]

[[end]][[if .Text]]<project_data>
[[.Text]]
</project_data>[[else]]Project [[printf "%q" .Name]] has no folder: it is a helper working across the person's whole machine.[[end]]

[[if .Goal]]Goal described by the person: [[.Goal]]

[[end]]Return JSON matching the schema:
[[.Schema]]
