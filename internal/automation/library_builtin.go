package automation

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	librarytemplates "bitbucket.org/senprints/agent-office/templates/library"
)

// SeedBuiltinSkills copies the shipped starter skills into the office library
// when they are missing. It deliberately never overwrites a person's copy:
// the library is editable and a future office upgrade must not discard edits.
func (l Library) SeedBuiltinSkills() (int, error) {
	entries, err := fs.Glob(librarytemplates.FS, "skills/*/SKILL.md")
	if err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, nil
	}
	if err := os.MkdirAll(l.sub("skill"), 0o755); err != nil {
		return 0, err
	}

	seeded := 0
	for _, entry := range entries {
		name := filepath.Base(filepath.Dir(filepath.FromSlash(entry)))
		if !nameRe.MatchString(name) {
			return seeded, errors.New("skill có tên không hợp lệ: " + name)
		}
		dir := l.path("skill", name)
		if _, err := os.Stat(dir); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return seeded, err
		}
		raw, err := fs.ReadFile(librarytemplates.FS, entry)
		if err != nil {
			return seeded, err
		}
		if err := writeFiles(dir, map[string]string{"SKILL.md": strings.TrimSpace(string(raw)) + "\n"}); err != nil {
			return seeded, err
		}
		seeded++
	}
	return seeded, nil
}
