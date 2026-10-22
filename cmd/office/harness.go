package main

import (
	"github.com/spf13/cobra"

	"bitbucket.org/senprints/agent-office/internal/harness"
)

// office harness: the evidence for a project's harness review (ADR-181) as
// JSON, with no AI and no token; the better-harness workflow's first step.
func harnessCmd() *cobra.Command {
	var days int
	cmd := &cobra.Command{
		Use:     "harness <project>",
		Short:   "Bằng chứng về vòng làm việc của agent trong project (JSON, cho quy trình better-harness)",
		Example: `  office harness shop --days 30`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withApp(cmd.Context(), func(a *app) error {
				p, err := findProject(cmd, a, args[0])
				if err != nil {
					return err
				}
				ev, err := harness.Collect(cmd.Context(), a.store, p, days)
				if err != nil {
					return err
				}
				return printJSON(ev)
			})
		},
	}
	cmd.Flags().IntVar(&days, "days", 30, "số ngày gần nhất (1-90)")
	return cmd
}
