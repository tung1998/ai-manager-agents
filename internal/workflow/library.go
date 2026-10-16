package workflow

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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

// Renamed maps legacy slash-command keys to their shipped workflow keys.
// It remains for commands saved before the workflows were renamed.
var Renamed = map[string]string{
	"giao-lai": "handoff", "co-van": "advisor", "hoi-dong": "council", "hoi-dong-3-ben": "council-3",
	"lam-tinh-nang": "feature", "sua-bug": "bugfix", "viet-noi-dung": "write-content",
}

// Library keeps user-created workflows as files: <dir>/<key>.md. Shipped
// workflows are read directly from builtinFS and are never copied here.
type Library struct{ Dir string }

// Item is one workflow of the library.
type Item struct {
	Def       Def       `json:"def"`
	Source    string    `json:"source,omitempty"`
	Builtin   bool      `json:"builtin"` // shipped with the office
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	Version   string    `json:"version"` // ADR-072: a content hash; "" means the file is gone
}

// ErrNotFound: no workflow of that key.
var ErrNotFound = errors.New("không có quy trình này trong thư viện")

// ErrBuiltinImmutable means a shipped workflow can only be changed in source
// and released as a new office version. A project gets its own editable copy.
var ErrBuiltinImmutable = errors.New("quy trình có sẵn chỉ đọc; hãy thêm vào project để tùy chỉnh")

// ErrConflict: a save over someone else's change (ADR-072); answered 409.
var ErrConflict = errors.New("Đã có thay đổi mới ở nơi khác kể từ lúc bạn mở (người khác hoặc agent). Tải lại để xem bản mới rồi sửa lại.")

// saveMu serializes every library write (Save, SaveIf, Delete) so a
// version check and the write that follows it see a consistent file, and no
// write can land between another write's check and its write.
var saveMu sync.Mutex

func (l Library) path(key string) string { return filepath.Join(l.Dir, key+".md") }

// List returns every workflow of the library (one that does not parse is
// listed with its error).
func (l Library) List() ([]Item, error) {
	builtins, err := Builtins()
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(builtins))
	for _, b := range builtins {
		out = append(out, Item{Def: b.Def, Builtin: true, Version: Hash(b.Source)})
	}
	entries, err := os.ReadDir(l.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		key, ok := strings.CutSuffix(e.Name(), ".md")
		if e.IsDir() || !ok || !ValidKey(key) {
			continue
		}
		if BuiltinSource(key) != "" {
			continue // ignore legacy mutable copies of shipped workflows
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
	if src := BuiltinSource(key); src != "" {
		d, err := Parse(src)
		if err != nil {
			return Item{}, err
		}
		return Item{Def: d, Source: src, Builtin: true, Version: Hash(src)}, nil
	}
	p := l.path(key)
	raw, err := os.ReadFile(p)
	if err != nil {
		return Item{}, ErrNotFound
	}
	st, _ := os.Stat(p)
	it := Item{Source: string(raw), Version: Hash(string(raw))}
	if st != nil {
		it.UpdatedAt = st.ModTime().UTC()
	}
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
	saveMu.Lock()
	defer saveMu.Unlock()
	return l.save(key, source)
}

// save is Save's write without the lock, for callers that already hold it.
func (l Library) save(key, source string) (Def, error) {
	if BuiltinSource(key) != "" {
		return Def{}, ErrBuiltinImmutable
	}
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

// SaveIf writes a workflow like Save, but refuses with ErrConflict when want
// is given and does not match the version read by a previous Get (want == ""
// skips the check, for tool/agent saves). The check and the write
// happen under one lock so two concurrent saves can't both pass it.
func (l Library) SaveIf(key, source, want string) (Item, error) {
	saveMu.Lock()
	defer saveMu.Unlock()
	if want != "" {
		cur, _ := l.Get(key) // err (not found/invalid) just means Version is ""
		if cur.Version != want {
			return Item{}, ErrConflict
		}
	}
	if _, err := l.save(key, source); err != nil {
		return Item{}, err
	}
	return l.Get(key)
}

// Delete removes a workflow from the library.
func (l Library) Delete(key string) error {
	saveMu.Lock()
	defer saveMu.Unlock()
	if !ValidKey(key) {
		return ErrNotFound
	}
	if BuiltinSource(key) != "" {
		return ErrBuiltinImmutable
	}
	if err := os.Remove(l.path(key)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	return nil
}
