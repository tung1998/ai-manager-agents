package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bitbucket.org/senprints/agent-office/internal/team"
)

func packCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "pack", Aliases: []string{"template"}, Short: "Gói khởi tạo: agent và quy trình cho project mới"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Liệt kê gói khởi tạo",
		RunE: func(cmd *cobra.Command, _ []string) error {
			list, err := team.Packs()
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "KEY\tTÊN\tAGENT\tQUY TRÌNH\tMÔ TẢ")
			for _, p := range list {
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", p.Key, p.Name, len(p.Agents), strings.Join(p.Workflows, ", "), truncateRunes(p.Description, 60))
			}
			return w.Flush()
		},
	})
	return cmd
}
