The admin's instructions for this automation (follow them exactly):
[[.Instructions]]

[[if .Command]][[if .Text]][[.Who]] used the command /[[.Command]] with:
[[.Text]][[else]][[.Who]] used the command /[[.Command]], with nothing after it.[[end]]
[[- else if .ShowMessage]]Message from [[.Who]]:
[[.Text]][[end]]
