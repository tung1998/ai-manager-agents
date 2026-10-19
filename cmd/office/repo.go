package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bitbucket.org/senprints/agent-office/internal/repos"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/team"
)

func repoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "project",
		Aliases: []string{"repo", "projects"},
		Short:   "Quản lý project (thư mục trên máy, hoặc helper toàn máy)",
	}

	var (
		name      string
		agents    []string
		workflows []string
	)
	add := &cobra.Command{
		Use:   "add [đường-dẫn]",
		Short: "Thêm project; bỏ trống đường dẫn để tạo helper cho toàn bộ máy",
		Example: `  office project add ~/code/shop --agent office/team-lead --agent office/frontend-dev --workflow review-pr
  office project add --name "Trợ lý máy"   # không gắn thư mục`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var info repos.Info
			if len(args) == 1 {
				var err error
				if info, err = repos.Detect(args[0]); err != nil {
					return err
				}
			} else if strings.TrimSpace(name) == "" {
				return errors.New("project không có thư mục cần --name")
			}
			if name != "" {
				info.Name = name
			}
			for _, id := range agents { // checked first: a wrong id leaves no project behind
				if _, err := team.LibraryAgentByID(id); err != nil {
					return err
				}
			}
			return withApp(cmd.Context(), func(a *app) error {
				r, err := a.store.Repos().Create(cmd.Context(), storage.Repo{Name: info.Name, Path: info.Path, GitRemote: info.GitRemote, Description: info.Description})
				if errors.Is(err, storage.ErrConflict) {
					return fmt.Errorf("thư mục %s đã là project", info.Path)
				}
				if err != nil {
					return err
				}
				fmt.Printf("✓ Đã thêm project %s · %s\n", r.Name, projectScope(r))
				ids := agents
				if len(ids) == 0 {
					ids = []string{team.StarterAgent}
				}
				if err := a.team.AddFromLibrary(cmd.Context(), r.ID, ids...); err != nil {
					return err
				}
				fmt.Printf("✓ Agent từ thư viện: %s\n", strings.Join(ids, ", "))
				if len(workflows) > 0 {
					if err := a.team.InstallWorkflows(cmd.Context(), r.ID, workflows); err != nil {
						return err
					}
					fmt.Printf("✓ Quy trình: %s\n", strings.Join(workflows, ", "))
				}
				return nil
			})
		},
	}
	add.Flags().StringArrayVar(&agents, "agent", nil, "agent từ thư viện theo id, vd office/devops (lặp lại được; mặc định office/assistant)")
	add.Flags().StringSliceVar(&workflows, "workflow", nil, "cài quy trình từ thư viện theo key, vd review-pr,fix-tests")
	add.Flags().StringVar(&name, "name", "", "tên project (bắt buộc khi không có đường dẫn)")

	list := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withApp(cmd.Context(), func(a *app) error {
				rs, err := a.store.Repos().List(cmd.Context())
				if err != nil {
					return err
				}
				w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
				fmt.Fprintln(w, "TÊN\tMẶC ĐỊNH\tAGENT\tPHẠM VI")
				for _, r := range rs {
					agents, _ := a.store.Agents().List(cmd.Context(), r.ID)
					def := "—"
					if d, ok := storage.DefaultAgent(r, agents); ok {
						def = d.Name
					}
					fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", r.Name, def, len(agents), projectScope(r))
				}
				return w.Flush()
			})
		},
	}

	rm := &cobra.Command{
		Use:   "rm <đường-dẫn|tên>",
		Short: "Bỏ quản lý project (không xóa file)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withApp(cmd.Context(), func(a *app) error {
				r, err := findProject(cmd, a, args[0])
				if err != nil {
					return err
				}
				if err := a.store.Repos().Delete(cmd.Context(), r.ID); err != nil {
					return err
				}
				fmt.Printf("✓ Đã bỏ quản lý %s\n", r.Name)
				return nil
			})
		},
	}
	cmd.AddCommand(add, list, rm)
	return cmd
}

// findProject matches a folder path first, then an exact project name.
func findProject(cmd *cobra.Command, a *app, ref string) (storage.Repo, error) {
	if info, err := repos.Detect(ref); err == nil {
		if r, err := a.store.Repos().GetByPath(cmd.Context(), info.Path); err == nil {
			return r, nil
		}
	}
	rs, err := a.store.Repos().List(cmd.Context())
	if err != nil {
		return storage.Repo{}, err
	}
	for _, r := range rs {
		if strings.EqualFold(r.Name, ref) || r.ID == ref {
			return r, nil
		}
	}
	return storage.Repo{}, fmt.Errorf("không có project %q", ref)
}

func projectScope(r storage.Repo) string {
	if r.Path == "" {
		return "toàn máy (không gắn thư mục)"
	}
	return r.Path
}
