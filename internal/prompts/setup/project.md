[[if .Current]]The project's agents now (they stay as they are; never pick a twin of one):
[[.Current]]

[[end]]Agent library (pick "agents" from this list, by id):
[[.Agents]]

[[if .Workflows]]Workflow library (pick "workflows" from this list, by key):
[[.Workflows]]

[[end]][[if .Automations]]Automation templates (pick "automations" from this list, by key):
[[.Automations]]

[[end]][[if .Commands]]Project's command catalog (only pick "commands" from this list):
[[.Commands]]

[[end]][[if .Skills]]Skills in the office library (pick "skills" from this list, by name):
[[.Skills]]

[[end]][[if .MCP]]MCP servers office can add (pick "mcp" from this list, by name; needs key = a person must paste a key, needs login = sign in on first use):
[[.MCP]]

[[end]][[if .Text]]<project_data>
[[.Text]]
</project_data>[[else]]Project [[printf "%q" .Name]] has no folder: it is a helper working across the person's whole machine.[[end]]

[[if .Goal]]Goal described by the person: [[.Goal]]

[[end]]Return JSON matching the schema:
[[.Schema]]
