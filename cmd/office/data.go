package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"bitbucket.org/senprints/agent-office/internal/collections"
	"bitbucket.org/senprints/agent-office/internal/storage"
)

// office data: a project's data collections from a script (ADR-172), with
// no AI and no token: an automation's script reads and writes them.
func dataCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "data", Short: "Đọc, ghi bảng dữ liệu (collection) của project từ script"}

	list := &cobra.Command{
		Use:   "list <project>",
		Short: "Các collection của project và trường của chúng",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withApp(cmd.Context(), func(a *app) error {
				p, err := findProject(cmd, a, args[0])
				if err != nil {
					return err
				}
				list, err := collections.New(a.store).List(cmd.Context(), p.ID)
				if err != nil {
					return err
				}
				return printJSON(list)
			})
		},
	}

	var (
		where  []string
		search string
		sort   string
		desc   bool
		limit  int
	)
	query := &cobra.Command{
		Use:     "query <project> <collection>",
		Short:   "Tìm bản ghi (JSON ra stdout)",
		Example: `  office data query shop customers --where tier=vip --where "orders>=3" --search nguyen --limit 20`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := collections.Query{Search: search, Sort: sort, Desc: desc, Limit: limit}
			for _, w := range where {
				c, err := parseWhere(w)
				if err != nil {
					return err
				}
				q.Where = append(q.Where, c)
			}
			return withApp(cmd.Context(), func(a *app) error {
				p, err := findProject(cmd, a, args[0])
				if err != nil {
					return err
				}
				recs, total, err := collections.New(a.store).Query(cmd.Context(), p.ID, args[1], q)
				if err != nil {
					return err
				}
				return printJSON(map[string]any{"total": total, "records": recs})
			})
		},
	}
	query.Flags().StringArrayVar(&where, "where", nil, "lọc: field=value, field!=value, field>=value, field<value, field~value (chứa)")
	query.Flags().StringVar(&search, "search", "", "tìm chữ (không dấu)")
	query.Flags().StringVar(&sort, "sort", "", "sắp theo trường (mặc định updated_at)")
	query.Flags().BoolVar(&desc, "desc", false, "giảm dần")
	query.Flags().IntVar(&limit, "limit", 50, "số bản ghi tối đa (≤200)")

	var id, data string
	upsert := &cobra.Command{
		Use:   "upsert <project> <collection>",
		Short: "Ghi một bản ghi: có --id thì sửa, không thì khớp trường unique hoặc tạo mới",
		Example: `  office data upsert shop customers --data '{"name":"An","email":"an@x.vn"}'
  echo '{"orders":4}' | office data upsert shop customers --id rec_… --data -`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw := []byte(data)
			if data == "-" {
				var err error
				if raw, err = io.ReadAll(os.Stdin); err != nil {
					return err
				}
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				return errors.New("--data cần một object JSON")
			}
			return withApp(cmd.Context(), func(a *app) error {
				p, err := findProject(cmd, a, args[0])
				if err != nil {
					return err
				}
				x, created, err := collections.New(a.store).Upsert(cmd.Context(), p.ID, args[1], id, m, "script")
				if err != nil {
					return err
				}
				return printJSON(map[string]any{"created": created, "record": x})
			})
		},
	}
	upsert.Flags().StringVar(&id, "id", "", "id bản ghi cần sửa")
	upsert.Flags().StringVar(&data, "data", "", "các trường, JSON (- = đọc stdin)")
	_ = upsert.MarkFlagRequired("data")

	cmd.AddCommand(list, query, upsert)
	return cmd
}

// parseWhere reads field<op>value (=, !=, >=, <=, >, <, ~).
func parseWhere(w string) (storage.Cond, error) {
	for _, op := range []struct{ sym, name string }{{"!=", "ne"}, {">=", "gte"}, {"<=", "lte"}, {"~", "contains"}, {"=", "eq"}, {">", "gt"}, {"<", "lt"}} {
		if f, v, ok := strings.Cut(w, op.sym); ok && strings.TrimSpace(f) != "" {
			var val any = strings.TrimSpace(v)
			var n json.Number
			if json.Unmarshal([]byte(v), &n) == nil {
				val, _ = n.Float64()
			} else if v == "true" || v == "false" {
				val = v == "true"
			}
			return storage.Cond{Field: strings.TrimSpace(f), Op: op.name, Value: val}, nil
		}
	}
	return storage.Cond{}, fmt.Errorf("--where %q: cần field=value", w)
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
