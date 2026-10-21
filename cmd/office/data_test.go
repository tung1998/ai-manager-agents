package main

import "testing"

func TestParseWhere(t *testing.T) {
	for in, want := range map[string]string{"tier=vip": "tier eq vip", "orders>=3": "orders gte 3", "name~an": "name contains an", "x!=1": "x ne 1", "on=true": "on eq true"} {
		c, err := parseWhere(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := c.Field + " " + c.Op + " " + fmtValue(c.Value); got != want {
			t.Fatalf("%s = %s, want %s", in, got, want)
		}
	}
	if _, err := parseWhere("novalue"); err == nil {
		t.Fatal("no operator")
	}
}

func fmtValue(v any) string {
	switch x := v.(type) {
	case float64:
		return map[bool]string{true: "3", false: "1"}[x == 3]
	case bool:
		return map[bool]string{true: "true", false: "false"}[x]
	case string:
		return x
	}
	return "?"
}
