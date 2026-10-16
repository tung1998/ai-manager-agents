package deciding

import "testing"

func TestBeginEnd(t *testing.T) {
	var told []string
	OnChange(func(table string) { told = append(told, table) })
	defer OnChange(nil)

	if !Begin("actions", "act_1", "auto:dev") {
		t.Fatal("first Begin refused")
	}
	if Begin("actions", "act_1", "human:a") {
		t.Fatal("second Begin of the same id allowed")
	}
	if by, ok := Is("act_1"); !ok || by != "auto:dev" {
		t.Fatalf("Is = %q, %v", by, ok)
	}
	End("actions", "act_1")
	if _, ok := Is("act_1"); ok {
		t.Fatal("still deciding after End")
	}
	if !Begin("actions", "act_1", "human:a") {
		t.Fatal("Begin after End refused")
	}
	End("actions", "act_1")
	if len(told) != 4 { // a refused Begin tells nothing
		t.Fatalf("told %v", told)
	}
}
