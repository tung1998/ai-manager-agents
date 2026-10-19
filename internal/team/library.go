package team

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/templates/library"
)

// LibraryCategories are the kinds of work library agents are grouped by
// (ADR-090, plus design), in display order.
var LibraryCategories = []string{"code", "design", "plan", "customer", "content"}

// LibraryEntry is one library agent without its instructions: the list a
// person browses (ADR-154). The instructions are read when one is opened.
type LibraryEntry struct {
	ID          string `json:"id"`  // "<set>/<key>": office/devops, agency/frontend-developer
	Set         string `json:"set"` // office (written for office) | agency (imported)
	Key         string `json:"key" yaml:"key"`
	Name        string `json:"name" yaml:"name"`
	Role        string `json:"role" yaml:"role"`
	Description string `json:"description" yaml:"description"`
	Category    string `json:"category" yaml:"category"`
	Emoji       string `json:"emoji,omitempty" yaml:"emoji"`
	ModelTier   string `json:"model_tier" yaml:"model_tier"`
	Level       string `json:"level" yaml:"level"`
	Featured    bool   `json:"featured,omitempty" yaml:"featured"`
	Source      string `json:"source,omitempty" yaml:"source"` // where it was adapted from
	Words       int    `json:"words"`                          // length of the instructions
}

// LibraryAgent is a library agent in full.
type LibraryAgent struct {
	LibraryEntry
	Instructions string              `json:"instructions"`
	Permissions  storage.Permissions `json:"permissions"`
}

// Spec is the library agent as one of a project's agents.
func (a LibraryAgent) Spec() AgentSpec {
	return AgentSpec{Key: a.Key, Name: a.Name, Role: a.Role, Description: a.Description,
		ModelTier: a.ModelTier, Instructions: a.Instructions, Permissions: a.Permissions}
}

// ErrNoLibraryAgent: no library agent has that id.
var ErrNoLibraryAgent = errors.New("không có agent này trong thư viện")

var (
	libOnce  sync.Once
	libIndex []LibraryEntry
	libErr   error
)

// Library lists the library agents (no instructions): the office's own
// first, then by category and name. Read once; the files are embedded.
func Library() ([]LibraryEntry, error) {
	libOnce.Do(func() {
		files, err := fs.Glob(library.FS, "*/*.md")
		if err != nil {
			libErr = err
			return
		}
		for _, f := range files {
			a, err := readLibrary(f)
			if err != nil {
				libErr = err
				return
			}
			libIndex = append(libIndex, a.LibraryEntry)
		}
		rank := func(c string) int {
			for i, x := range LibraryCategories {
				if x == c {
					return i
				}
			}
			return len(LibraryCategories)
		}
		sort.SliceStable(libIndex, func(i, j int) bool {
			a, b := libIndex[i], libIndex[j]
			if a.Featured != b.Featured {
				return a.Featured
			}
			if rank(a.Category) != rank(b.Category) {
				return rank(a.Category) < rank(b.Category)
			}
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		})
	})
	return libIndex, libErr
}

// LibraryAgentByID reads one library agent in full.
func LibraryAgentByID(id string) (LibraryAgent, error) {
	set, key, ok := strings.Cut(id, "/")
	if !ok || set == "" || strings.ContainsAny(key, `/\.`) || !ValidKey(key) {
		return LibraryAgent{}, fmt.Errorf("%w: %q", ErrNoLibraryAgent, id)
	}
	a, err := readLibrary(path.Join(set, key+".md"))
	if errors.Is(err, fs.ErrNotExist) {
		return LibraryAgent{}, fmt.Errorf("%w: %q", ErrNoLibraryAgent, id)
	}
	return a, err
}

// readLibrary parses one file: YAML frontmatter, then the instructions.
func readLibrary(name string) (LibraryAgent, error) {
	raw, err := fs.ReadFile(library.FS, name)
	if err != nil {
		return LibraryAgent{}, err
	}
	head, body, ok := bytes.Cut(bytes.TrimPrefix(raw, []byte("---\n")), []byte("\n---\n"))
	if !ok || !bytes.HasPrefix(raw, []byte("---\n")) {
		return LibraryAgent{}, fmt.Errorf("team: library %s: no frontmatter", name)
	}
	var a LibraryAgent
	if err := yaml.Unmarshal(head, &a.LibraryEntry); err != nil {
		return LibraryAgent{}, fmt.Errorf("team: library %s: %w", name, err)
	}
	a.Set = path.Dir(name)
	a.ID = a.Set + "/" + a.Key
	if path.Base(name) != a.Key+".md" {
		return LibraryAgent{}, fmt.Errorf("team: library %s: key %q does not match the file name", name, a.Key)
	}
	if a.ModelTier == "" {
		a.ModelTier = storage.TierBalanced
	}
	if a.Level == "" {
		a.Level = "read"
	}
	a.Instructions = strings.TrimSpace(string(body))
	a.Words = len(strings.Fields(a.Instructions))
	// ADR-074: a library agent never brings full access or extra folders
	a.Permissions = storage.Permissions{Level: a.Level, ReadOnly: a.Level == "read"}
	return a, nil
}
