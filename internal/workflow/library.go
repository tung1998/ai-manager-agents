package workflow

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

//go:embed builtin/*.md
var builtinFS embed.FS

// Builtin is a workflow shipped with the office.
type Builtin struct {
	Def    Def
	Source string // the whole file
}

// Builtins returns the shipped workflows, sorted by key order of use.
func Builtins() ([]Builtin, error) {
	entries, err := fs.ReadDir(builtinFS, "builtin")
	if err != nil {
		return nil, err
	}
	var out []Builtin
	for _, e := range entries {
		raw, err := fs.ReadFile(builtinFS, "builtin/"+e.Name())
		if err != nil {
			return nil, err
		}
		d, err := Parse(string(raw))
		if err != nil {
			return nil, errors.New(e.Name() + ": " + err.Error())
		}
		out = append(out, Builtin{Def: d, Source: string(raw)})
	}
	order := map[string]int{"handoff": 0, "advisor": 1, "council": 2}
	sort.SliceStable(out, func(i, j int) bool {
		oi, iok := order[out[i].Def.Key]
		oj, jok := order[out[j].Def.Key]
		switch {
		case iok && jok:
			return oi < oj
		case iok != jok:
			return iok
		}
		return out[i].Def.Key < out[j].Def.Key
	})
	return out, nil
}

// BuiltinSource is a shipped workflow's file ("" = none of that key).
func BuiltinSource(key string) string {
	raw, err := fs.ReadFile(builtinFS, "builtin/"+key+".md")
	if err != nil {
		return ""
	}
	return string(raw)
}

// Library keeps the office's workflows as files: <dir>/<key>.md.
type Library struct{ Dir string }

// Item is one workflow of the library.
type Item struct {
	Def       Def       `json:"def"`
	Source    string    `json:"source,omitempty"`
	Builtin   bool      `json:"builtin"`  // shipped with the office
	Modified  bool      `json:"modified"` // a shipped one, changed here
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ErrNotFound: no workflow of that key.
var ErrNotFound = errors.New("không có quy trình này trong thư viện")

func (l Library) path(key string) string { return filepath.Join(l.Dir, key+".md") }

// shippedBefore are the hashes (Hash) of earlier shipped versions of each
// workflow: a library file still exactly one of them was not changed here,
// so Seed brings it up to date. Add the old hash whenever a builtin changes.
var shippedBefore = map[string][]string{
	"advisor":       {"10880d9f499cd790"},
	"bugfix":        {"b92f60681164b12a"},
	"council-3":     {"31d3dbe01a69c284"},
	"council":       {"64b2ae32e4a621fe"},
	"feature":       {"b62577236d4c8b78"},
	"handoff":       {"3b2e7a5fe034b4cc"},
	"review-pr":     {"15be50acea4fd2dc"},
	"write-content": {"27ee98aa775be3b2"},
}

// Seed writes the shipped workflows that are not there yet, and updates the
// ones still as an earlier version shipped them (one deleted on
// purpose comes back only with Reset).
func (l Library) Seed() (int, error) {
	list, err := Builtins()
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(l.Dir, 0o755); err != nil {
		return 0, err
	}
	seen := l.seen()
	if err := l.renameOld(seen); err != nil {
		return 0, err
	}
	n := 0
	for _, b := range list {
		if raw, err := os.ReadFile(l.path(b.Def.Key)); err == nil {
			seen[b.Def.Key] = true
			// an earlier shipped version nobody changed takes the new one
			if h := Hash(string(raw)); h != Hash(b.Source) && slices.Contains(shippedBefore[b.Def.Key], h) {
				if err := os.WriteFile(l.path(b.Def.Key), []byte(b.Source), 0o644); err != nil {
					return n, err
				}
				n++
			}
			continue
		}
		if seen[b.Def.Key] {
			continue // deleted on purpose
		}
		if err := os.WriteFile(l.path(b.Def.Key), []byte(b.Source), 0o644); err != nil {
			return n, err
		}
		seen[b.Def.Key] = true
		n++
	}
	return n, l.saveSeen(seen)
}

// Renamed are shipped workflows whose key changed (Vietnamese → English
// commands); the key stays in each file's header.
var Renamed = map[string]string{
	"giao-lai": "handoff", "co-van": "advisor", "hoi-dong": "council", "hoi-dong-3-ben": "council-3",
	"lam-tinh-nang": "feature", "sua-bug": "bugfix", "viet-noi-dung": "write-content",
}

// renameOld moves a shipped workflow written under its old key to the new
// one, keeping what was changed in it; one already there under the new key
// leaves the old file as the person's own.
func (l Library) renameOld(seen map[string]bool) error {
	for old, key := range Renamed {
		if seen[old] {
			delete(seen, old)
			seen[key] = true
		}
		raw, err := os.ReadFile(l.path(old))
		if err != nil {
			continue
		}
		if _, err := os.Stat(l.path(key)); err == nil {
			continue
		}
		src := regexp.MustCompile(`(?m)^key:\s*`+regexp.QuoteMeta(old)+`\s*$`).ReplaceAllString(string(raw), "key: "+key)
		if err := os.WriteFile(l.path(key), []byte(src), 0o644); err != nil {
			return err
		}
		if err := os.Remove(l.path(old)); err != nil {
			return err
		}
	}
	return nil
}

// seen are the shipped workflows already written once (a deleted one stays deleted).
func (l Library) seen() map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(filepath.Join(l.Dir, ".seeded"))
	if err != nil {
		return out
	}
	for _, k := range strings.Fields(string(raw)) {
		out[k] = true
	}
	return out
}

func (l Library) saveSeen(m map[string]bool) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return os.WriteFile(filepath.Join(l.Dir, ".seeded"), []byte(strings.Join(keys, "\n")+"\n"), 0o644)
}

// List returns every workflow of the library (one that does not parse is
// listed with its error).
func (l Library) List() ([]Item, error) {
	entries, err := os.ReadDir(l.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Item{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Item{}
	for _, e := range entries {
		key, ok := strings.CutSuffix(e.Name(), ".md")
		if e.IsDir() || !ok || !ValidKey(key) {
			continue
		}
		it, err := l.Get(key)
		if err != nil && it.Source == "" {
			continue
		}
		it.Source = ""
		out = append(out, it)
	}
	return out, nil
}

// Get loads one workflow; a file that does not parse comes back with its
// source and the error.
func (l Library) Get(key string) (Item, error) {
	if !ValidKey(key) {
		return Item{}, ErrNotFound
	}
	p := l.path(key)
	raw, err := os.ReadFile(p)
	if err != nil {
		return Item{}, ErrNotFound
	}
	st, _ := os.Stat(p)
	it := Item{Source: string(raw), Builtin: BuiltinSource(key) != ""}
	if st != nil {
		it.UpdatedAt = st.ModTime().UTC()
	}
	it.Modified = it.Builtin && strings.TrimSpace(BuiltinSource(key)) != strings.TrimSpace(string(raw))
	d, err := Parse(string(raw))
	it.Def = d
	if err != nil {
		it.Def.Key = key
		it.Error = err.Error()
		return it, err
	}
	return it, nil
}

// Save writes a workflow; its header key must be the file's.
func (l Library) Save(key, source string) (Def, error) {
	d, err := Parse(source)
	if err != nil {
		return d, err
	}
	if d.Key != key {
		return d, invalid("key trong phần đầu (%s) phải trùng tên quy trình (%s)", d.Key, key)
	}
	if err := os.MkdirAll(l.Dir, 0o755); err != nil {
		return d, err
	}
	return d, os.WriteFile(l.path(key), []byte(strings.TrimSpace(source)+"\n"), 0o644)
}

// Delete removes a workflow from the library.
func (l Library) Delete(key string) error {
	if !ValidKey(key) {
		return ErrNotFound
	}
	if err := os.Remove(l.path(key)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// Reset puts a shipped workflow back as it shipped.
func (l Library) Reset(key string) (Def, error) {
	src := BuiltinSource(key)
	if src == "" {
		return Def{}, errors.New("chỉ khôi phục được quy trình có sẵn")
	}
	return l.Save(key, src)
}
