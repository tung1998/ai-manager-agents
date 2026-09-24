package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"bitbucket.org/senprints/agent-office/internal/home"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/repos"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

func initCmd() *cobra.Command {
	var (
		local    bool
		template string
		replace  bool
		yes      bool
	)
	cmd := &cobra.Command{
		Use:   "init [đường-dẫn]",
		Short: "Thêm thư mục làm project và chọn mô hình tổ chức agent",
		Long: `Thêm một thư mục làm project để office quản lý và áp một mô hình tổ chức (solo, team, council, hoặc mẫu tự tạo).

Hai chế độ cài đặt:
  --local   dữ liệu nằm trong <project>/.office, office chỉ quản lý project này
  mặc định  dữ liệu ở ~/.agent-office (hoặc .office của project gần nhất), quản lý nhiều project ở bất kỳ đâu`,
		Example: `  office init                       # thư mục hiện tại, chọn mô hình tương tác
  office init --local --template solo
  office init ~/code/shop --template team`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			info, err := repos.Detect(path)
			if err != nil {
				return fmt.Errorf("không đọc được thư mục %s: %w", path, err)
			}

			var h home.Home
			if local {
				h, err = home.LocalHome(info.Path)
			} else {
				h, err = resolveHome()
			}
			if err != nil {
				return err
			}
			a, err := openApp(ctx, h)
			if err != nil {
				return err
			}
			defer a.Close()
			fmt.Fprintf(os.Stderr, "Dữ liệu office: %s (chế độ %s)\n", h.Dir, h.Mode)
			if h.Mode == home.Local {
				ensureGitignore(h.ProjectRoot)
			}

			repo, err := a.store.Repos().GetByPath(ctx, info.Path)
			if errors.Is(err, storage.ErrNotFound) {
				repo, err = a.store.Repos().Create(ctx, storage.Repo{Name: info.Name, Path: info.Path, GitRemote: info.GitRemote, Description: info.Description})
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "✓ Đã thêm project %s (%s)\n", repo.Name, repo.Path)
			} else if err != nil {
				return err
			} else {
				fmt.Fprintf(os.Stderr, "• Project %s đã có\n", repo.Name)
			}

			current, err := a.store.OrgModels().GetForRepo(ctx, repo.ID)
			hasModel := err == nil
			if hasModel && !replace && template == "" {
				fmt.Fprintf(os.Stderr, "• Project đang dùng mô hình %q (dùng --template ... --replace để đổi)\n", current.Name)
			} else {
				tpl, err := pickTemplate(cmd, a, template)
				if err != nil {
					return err
				}
				if hasModel && !replace {
					if !interactive() || !confirm(fmt.Sprintf("Project đang dùng %q. Thay bằng %q?", current.Name, tpl.Name), false) {
						return errors.New("project đã có mô hình; thêm --replace để thay")
					}
				}
				m, err := a.org.ApplyToRepo(ctx, repo.ID, tpl.ID, true)
				if err != nil {
					return err
				}
				agents, _ := a.store.Agents().List(ctx, m.ID)
				fmt.Fprintf(os.Stderr, "✓ Áp mô hình %s: %d agent\n", m.Name, len(agents))
				for _, ag := range agents {
					fmt.Fprintf(os.Stderr, "    %-8s %-18s %s\n", ag.Tier, ag.Key, ag.Role)
				}
			}

			if err := detectProviders(cmd, a, yes); err != nil {
				return err
			}

			n, _ := a.store.Users().Count(ctx)
			fmt.Fprintln(os.Stderr, "\nTiếp theo:")
			step := 1
			if n == 0 {
				fmt.Fprintf(os.Stderr, "  %d. %s user create --email you@company.com --role admin\n", step, homeHint(h))
				step++
			}
			fmt.Fprintf(os.Stderr, "  %d. %s run            # mở API cho dashboard\n", step, homeHint(h))
			fmt.Fprintf(os.Stderr, "  %d. Vào dashboard → Kết nối AI / Project / Mô hình để chỉnh chi tiết\n", step+1)
			return nil
		},
	}
	cmd.Flags().BoolVar(&local, "local", false, "lưu dữ liệu trong <project>/.office (chỉ quản lý project này)")
	cmd.Flags().StringVar(&template, "template", "", "key mô hình: solo | team | council | mẫu tự tạo")
	cmd.Flags().BoolVar(&replace, "replace", false, "thay mô hình hiện tại của project")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "tự đồng ý các bước (không hỏi)")
	return cmd
}

func pickTemplate(cmd *cobra.Command, a *app, key string) (storage.OrgModel, error) {
	ctx := cmd.Context()
	if key != "" {
		m, err := a.store.OrgModels().GetTemplateByKey(ctx, key)
		if errors.Is(err, storage.ErrNotFound) {
			return m, fmt.Errorf("không có mô hình mẫu %q (xem: office template list)", key)
		}
		return m, err
	}
	list, err := a.store.OrgModels().ListTemplates(ctx)
	if err != nil || len(list) == 0 {
		return storage.OrgModel{}, fmt.Errorf("chưa có mô hình mẫu: %v", err)
	}
	if !interactive() {
		return list[0], nil
	}
	opts := make([]string, len(list))
	for i, m := range list {
		agents, _ := a.store.Agents().List(ctx, m.ID)
		opts[i] = fmt.Sprintf("%-20s %d agent · %s", m.Name, len(agents), truncateRunes(m.Description, 70))
	}
	return list[choose("Chọn mô hình tổ chức:", opts, 0)], nil
}

// detectProviders offers to create connections for CLIs and API keys found on this machine.
func detectProviders(cmd *cobra.Command, a *app, yes bool) error {
	ctx := cmd.Context()
	existing, err := a.store.Providers().List(ctx)
	if err != nil {
		return err
	}
	have := map[storage.ProviderKind]bool{}
	for _, p := range existing {
		have[p.Kind] = true
	}
	type cand struct {
		label string
		in    provider.Input
	}
	var cands []cand
	if _, err := exec.LookPath("claude"); err == nil && !have[storage.ProviderClaudeCLI] {
		cands = append(cands, cand{"Claude Code CLI (lệnh claude trên máy)", provider.Input{Name: "Claude Code CLI", Kind: storage.ProviderClaudeCLI}})
	}
	if _, err := exec.LookPath("codex"); err == nil && !have[storage.ProviderCodexCLI] {
		cands = append(cands, cand{"Codex CLI (lệnh codex trên máy)", provider.Input{Name: "Codex CLI", Kind: storage.ProviderCodexCLI}})
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" && !have[storage.ProviderAnthropic] {
		cands = append(cands, cand{"Claude API (đọc ANTHROPIC_API_KEY)", provider.Input{Name: "Claude API", Kind: storage.ProviderAnthropic, APIKeyEnv: "ANTHROPIC_API_KEY"}})
	}
	if os.Getenv("OPENAI_API_KEY") != "" && !have[storage.ProviderOpenAI] {
		cands = append(cands, cand{"OpenAI API (đọc OPENAI_API_KEY)", provider.Input{Name: "OpenAI API", Kind: storage.ProviderOpenAI, APIKeyEnv: "OPENAI_API_KEY"}})
	}
	if len(cands) == 0 {
		if len(existing) == 0 {
			fmt.Fprintln(os.Stderr, "• Chưa có kết nối AI. Thêm trong dashboard hoặc: office provider add --kind anthropic --name \"Claude API\"")
		}
		return nil
	}
	for _, c := range cands {
		if !yes && (!interactive() || !confirm("Phát hiện "+c.label+". Thêm kết nối?", true)) {
			continue
		}
		p, err := a.providers.Create(ctx, c.in)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: %v\n", c.label, err)
			continue
		}
		res, _ := a.providers.Test(ctx, p.ID, "", "")
		status := "✓"
		if !res.OK {
			status = "!"
		}
		fmt.Fprintf(os.Stderr, "%s Kết nối %s: %s\n", status, p.Name, res.Detail)
	}
	return nil
}

func homeHint(h home.Home) string {
	if h.Mode == home.Local {
		return "office"
	}
	return "office"
}

// ensureGitignore keeps the local office data (DB, secret key) out of git.
func ensureGitignore(root string) {
	p := filepath.Join(root, ".gitignore")
	raw, _ := os.ReadFile(p)
	for _, line := range splitLines(string(raw)) {
		if line == ".office/" || line == ".office" || line == "/.office" || line == "/.office/" {
			return
		}
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	prefix := ""
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		prefix = "\n"
	}
	fmt.Fprintf(f, "%s# agent-office local data (database, secret key)\n.office/\n", prefix)
	fmt.Fprintln(os.Stderr, "✓ Thêm .office/ vào .gitignore")
}
