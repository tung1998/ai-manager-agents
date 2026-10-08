Starter packs (valid pack_key values):
[[.Packs]]

[[if .Text]]<project_data>
[[.Text]]
</project_data>[[else]]Project [[printf "%q" .Name]] has no folder: it is a helper working across the person's whole machine.[[end]]

[[if .Goal]]Goal described by the person: [[.Goal]]

[[end]]Return JSON matching the schema:
[[.Schema]]
