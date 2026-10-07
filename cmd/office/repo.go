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
)

func repoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "project",
		Aliases: []string{"repo", "projects"},
		Short:   "Quản lý project (thư mục trên máy, hoặc helper toàn máy)",
	}

	var pack, name string
	add := &cobra.Command{
		Use:   "add [đường-dẫn]",
		Short: "Thêm project; bỏ trống đường dẫn để tạo helper cho toàn bộ máy",
		Example: `  office project add ~/code/shop --pack team
  office project add --name "Trợ lý máy" --pack solo   # không gắn thư mục`,
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
			return withApp(cmd.Context(), func(a *app) error {
				r, err := a.store.Repos().Create(cmd.Context(), storage.Repo{Name: info.Name, Path: info.Path, GitRemote: info.GitRemote, Description: info.Description})
				if errors.Is(err, storage.ErrConflict) {
					return fmt.Errorf("thư mục %s đã là project", info.Path)
				}
				if err != nil {
					return err
				}
				fmt.Printf("✓ Đã thêm project %s · %s\n", r.Name, projectScope(r))
				if pack != "" {
					p, err := pickPack(pack)
					if err != nil {
						return err
					}
					if err := a.team.ApplyPack(cmd.Context(), r.ID, p, false); err != nil {
						return err
					}
					fmt.Printf("✓ Dùng gói %s: %d agent\n", p.Name, len(p.Agents))
				}
				return nil
			})
		},
	}
	add.Flags().StringVar(&pack, "pack", "", "dùng gói khởi tạo ngay: solo | team | council")
	add.Flags().StringVar(&pack, "template", "", "tên cũ của --pack")
	_ = add.Flags().MarkHidden("template")
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
