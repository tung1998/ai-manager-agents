package perm

import (
	"fmt"
	"strings"
)

// QuickCheckRule is a project's own quick check (ADR-135): files ending in
// one of Exts are checked by Command ({file} = the edited file, else it is
// added at the end), right after the built-in check.
type QuickCheckRule struct {
	Exts    []string
	Command string
}

// maxQuickChecks caps what the person writes.
const maxQuickChecks = 2000

// ParseQuickChecks reads a project's quick checks: one a line,
// ".ts .vue: npx eslint {file}"; empty lines and # comments skipped.
func ParseQuickChecks(s string) ([]QuickCheckRule, error) {
	if len([]rune(s)) > maxQuickChecks {
		return nil, fmt.Errorf("lệnh kiểm tra nhanh dài quá %d ký tự", maxQuickChecks)
	}
	var out []QuickCheckRule
	for i, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		exts, cmd, ok := strings.Cut(l, ":")
		cmd = strings.TrimSpace(cmd)
		if !ok || cmd == "" {
			return nil, fmt.Errorf("dòng %d: cần dạng \".ts .vue: lệnh {file}\"", i+1)
		}
		var r QuickCheckRule
		for _, e := range strings.Fields(strings.ReplaceAll(exts, ",", " ")) {
			if !strings.HasPrefix(e, ".") || strings.ContainsAny(e, `/\'"`) {
				return nil, fmt.Errorf("dòng %d: đuôi file %q phải bắt đầu bằng dấu chấm", i+1, e)
			}
			r.Exts = append(r.Exts, strings.ToLower(e))
		}
		if len(r.Exts) == 0 {
			return nil, fmt.Errorf("dòng %d: thiếu đuôi file", i+1)
		}
		r.Command = cmd
		out = append(out, r)
	}
	return out, nil
}
