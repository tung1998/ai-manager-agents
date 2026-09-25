package tasks

import (
	"strings"
	"testing"
)

func TestValidatePlan(t *testing.T) {
	canEdit := func(a string) bool { return a != "reader" }
	ok := Plan{Conventions: "dùng @nuxtjs/i18n, key dạng nav.x", Assignments: []Assignment{
		{Agent: "w1", Task: "cài i18n", Files: []string{"nuxt.config.ts", "package.json"}},
		{Agent: "w2", Task: "dịch layout", Files: []string{"app/layouts/default.vue"}, DependsOn: []int{1}},
		{Agent: "reader", Task: "đọc"},
	}}
	if p := validatePlan(ok, canEdit); len(p) != 0 {
		t.Fatalf("valid plan: %v", p)
	}
	bad := Plan{Assignments: []Assignment{
		{Agent: "w1", Task: "a", Files: []string{"./a.vue", "b.vue", "c.vue", "d.vue"}},
		{Agent: "w2", Task: "b", Files: []string{"a.vue"}, DependsOn: []int{2}},
		{Agent: "w3", Task: "c"},
	}}
	got := strings.Join(validatePlan(bad, canEdit), " | ")
	for _, want := range []string{"tối đa 3", "a.vue nằm trong cả việc 1 và việc 2", "việc 2 phụ thuộc việc 2", "không liệt kê files", "thiếu conventions"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}
