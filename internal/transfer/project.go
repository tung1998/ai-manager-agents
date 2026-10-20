package transfer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/repos"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/team"
)

// SetCloneRoot is where a new project's folder goes (cloned or empty).
func (s *Service) SetCloneRoot(dir string) { s.cloneRoot = dir }

// CloneRoot is where a new project's folder goes.
func (s *Service) CloneRoot() string {
	if s.cloneRoot != "" {
		return s.cloneRoot
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return "/"
}

var dirRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// DirName is a folder name for a project called name.
func DirName(name string) string {
	return strings.Trim(dirRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-"), "-.")
}

// CheckNewProject checks np before a person approves it, and fills its pack's
// name, description and git remote when it imports one.
func (s *Service) CheckNewProject(ctx context.Context, np *storage.NewProject) error {
	if np.Pack != "" {
		p, err := ReadPackFile(np.Pack)
		if err != nil {
			return err
		}
		np.Name = orStr(strings.TrimSpace(np.Name), p.Project.Name)
		np.Description = orStr(np.Description, p.Project.Description)
		if np.URL == "" && np.Path == "" {
			np.URL = p.Project.GitRemote
		}
		np.Include = CleanIncludes(orSlice(np.Include, p.Include))
	}
	switch {
	case np.URL != "" && np.Path != "":
		return errors.New("chọn một: clone từ url, hoặc thư mục có sẵn (path)")
	case np.URL != "":
		if _, err := repos.CloneURL(np.URL); err != nil {
			return err
		}
	case np.Path != "":
		if !filepath.IsAbs(np.Path) {
			return errors.New("path phải là đường dẫn tuyệt đối")
		}
		info, err := repos.Detect(np.Path) // as it is kept: absolute, links resolved
		if err != nil {
			return fmt.Errorf("không có thư mục %s", np.Path)
		}
		if _, err := s.store.Repos().GetByPath(ctx, info.Path); err == nil {
			return fmt.Errorf("thư mục %s đã là một project", np.Path)
		}
	default:
		if strings.TrimSpace(np.Name) == "" && np.Dir == "" {
			return errors.New("project mới cần tên (hoặc url để clone, path có sẵn)")
		}
	}
	return nil
}

func orSlice(a, b []string) []string {
	if len(a) > 0 {
		return a
	}
	return b
}

// CreateProject makes np (ADR-170): clones its URL, takes its Path, or
// starts an empty repo; then imports its pack, or gives it the starter agent.
func (s *Service) CreateProject(ctx context.Context, np storage.NewProject) (storage.Repo, error) {
	if err := s.CheckNewProject(ctx, &np); err != nil {
		return storage.Repo{}, err
	}
	var pack *Pack
	if np.Pack != "" {
		p, err := ReadPackFile(np.Pack)
		if err != nil {
			return storage.Repo{}, err
		}
		pack = &p
	}
	dir, made := np.Path, false
	switch {
	case np.URL != "":
		d, err := repos.Clone(ctx, np.URL, s.CloneRoot(), np.Dir)
		if err != nil {
			return storage.Repo{}, err
		}
		dir, made = d, true
	case np.Path == "":
		d, err := repos.Init(ctx, s.CloneRoot(), orStr(np.Dir, DirName(np.Name)))
		if err != nil {
			return storage.Repo{}, err
		}
		dir, made = d, true
	}
	undo := func() {
		if made {
			_ = os.RemoveAll(dir)
		}
	}
	info, err := repos.Detect(dir)
	if err != nil {
		undo()
		return storage.Repo{}, err
	}
	r, err := s.store.Repos().Create(ctx, storage.Repo{Name: orStr(strings.TrimSpace(np.Name), info.Name), Path: info.Path,
		GitRemote: info.GitRemote, Description: orStr(np.Description, info.Description)})
	if err != nil {
		undo()
		return storage.Repo{}, err
	}
	if pack != nil {
		_, err = s.ImportPack(ctx, *pack, r, np.Include)
	} else if s.team != nil {
		err = s.team.AddFromLibrary(ctx, r.ID, team.StarterAgent)
	}
	if err != nil {
		_ = s.store.Repos().Delete(ctx, r.ID)
		undo()
		return storage.Repo{}, err
	}
	return r, nil
}

// ReadPackFile reads a pack from a .zip on this machine.
func ReadPackFile(path string) (Pack, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Pack{}, fmt.Errorf("không đọc được %s", path)
	}
	if st.Size() > MaxPack {
		return Pack{}, errors.New("gói project quá lớn")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Pack{}, err
	}
	return ReadZip(data)
}
