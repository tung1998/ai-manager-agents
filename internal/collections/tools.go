package collections

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/officetools"
	"bitbucket.org/senprints/agent-office/internal/perm"
)

// Tools are the agents' data_* tools (an officetools.Provider): only in a
// project with a collection. Reading is for every agent; writing needs
// "propose" or more. Deleting is the person's, on the dashboard.
type Tools struct{ s *Service }

// NewTools gives s's tools to agents.
func NewTools(s *Service) Tools { return Tools{s} }

func obj(props map[string]any, required ...string) map[string]any {
	o := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

var collectionProp = str("The collection's key (see data_collections)")

// Tools lists the tools for sc.
func (t Tools) Tools(sc officetools.Scope) []officetools.Tool {
	if sc.ProjectID == "" {
		return nil
	}
	if list, err := t.s.List(context.Background(), sc.ProjectID); err != nil || len(list) == 0 {
		return nil
	}
	out := []officetools.Tool{
		{Name: "data_collections", ReadOnly: true, Schema: obj(map[string]any{}),
			Description: "The project's data collections (structured data kept in office): key, name, fields (name, type, required, unique, ref). Read this before data_query/data_upsert."},
		{Name: "data_query", ReadOnly: true,
			Description: "Find records of a collection: filters (op eq ne lt lte gt gte contains in), words searched with accents ignored, sort, page. Faster than reading files.",
			Schema: obj(map[string]any{"collection": collectionProp,
				"where": map[string]any{"type": "array", "description": "Filters, all must match", "items": obj(map[string]any{
					"field": str("Field name"), "op": str("eq ne lt lte gt gte contains in (default eq)"), "value": map[string]any{"description": "Value (a list for in)"}}, "field")},
				"search": str("Words to find in the text fields"), "sort": str("A field, created_at or updated_at (default: latest updated first)"),
				"desc": map[string]any{"type": "boolean"}, "limit": map[string]any{"type": "integer", "description": "Default 20, max 200"},
				"offset": map[string]any{"type": "integer"}}, "collection")},
		{Name: "data_get", ReadOnly: true, Description: "One record in full, the records its ref fields point to, and its latest changes.",
			Schema: obj(map[string]any{"collection": collectionProp, "id": str("Record id")}, "collection", "id")},
	}
	if perm.AtLeast(sc.Level, perm.Propose) {
		out = append(out, officetools.Tool{Name: "data_upsert",
			Description: "Write a record: with id, patch that record; else the record a unique field's value matches is patched, or a new one made. Only the fields given change; null takes a field out. Every change is kept in its history.",
			Schema: obj(map[string]any{"collection": collectionProp, "id": str("Record id to patch (optional)"),
				"data": map[string]any{"type": "object", "description": "Field values by name"}}, "collection", "data")})
	}
	return out
}

// Call runs one of the tools.
func (t Tools) Call(ctx context.Context, sc officetools.Scope, name string, raw json.RawMessage) (string, bool) {
	var in struct {
		Collection string         `json:"collection"`
		ID         string         `json:"id"`
		Data       map[string]any `json:"data"`
		Query
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return "Tham số không đọc được: " + err.Error(), true
		}
	}
	fail := func(err error) (string, bool) { return err.Error(), true }
	switch name {
	case "data_collections":
		list, err := t.s.List(ctx, sc.ProjectID)
		if err != nil {
			return fail(err)
		}
		var b strings.Builder
		for _, c := range list {
			fmt.Fprintf(&b, "## %s (%s)", c.Key, c.Name)
			if c.Pack != "" {
				fmt.Fprintf(&b, " · gói %s", c.Pack)
			}
			if c.Description != "" {
				fmt.Fprintf(&b, "\n%s", c.Description)
			}
			b.WriteString("\n")
			for _, f := range c.Fields {
				fmt.Fprintf(&b, "- %s: %s", f.Name, f.Type)
				var flags []string
				if f.Required {
					flags = append(flags, "required")
				}
				if f.Unique {
					flags = append(flags, "unique")
				}
				if len(f.Options) > 0 {
					flags = append(flags, "options "+strings.Join(f.Options, "|"))
				}
				if f.Ref != "" {
					flags = append(flags, "→ "+f.Ref)
				}
				if len(flags) > 0 {
					fmt.Fprintf(&b, " (%s)", strings.Join(flags, ", "))
				}
				b.WriteString("\n")
			}
		}
		return b.String(), false
	case "data_query":
		if in.Limit <= 0 {
			in.Limit = 20
		}
		list, total, err := t.s.Query(ctx, sc.ProjectID, in.Collection, in.Query)
		if err != nil {
			return fail(err)
		}
		return toJSON(map[string]any{"total": total, "records": list}), false
	case "data_get":
		x, refs, err := t.s.Get(ctx, sc.ProjectID, in.Collection, in.ID)
		if err != nil {
			return fail(err)
		}
		h, _ := t.s.History(ctx, sc.ProjectID, in.Collection, in.ID)
		if len(h) > 5 {
			h = h[:5]
		}
		return toJSON(map[string]any{"record": x, "refs": refs, "history": h}), false
	case "data_upsert":
		if !perm.AtLeast(sc.Level, perm.Propose) {
			return "Quyền hiện tại chỉ đọc dữ liệu.", true
		}
		x, created, err := t.s.Upsert(ctx, sc.ProjectID, in.Collection, in.ID, in.Data, "agent:"+sc.Agent)
		if err != nil {
			return fail(err)
		}
		return toJSON(map[string]any{"created": created, "record": x}), false
	}
	return "Không có công cụ " + name + ".", true
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
