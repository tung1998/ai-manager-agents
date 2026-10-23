package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"bitbucket.org/senprints/agent-office/internal/audit"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/gitops"
	"bitbucket.org/senprints/agent-office/internal/perm"
)

// The dashboard's file editor: an admin browses the project folder and edits
// a file right there, like an IDE (no worktree, no review). Ignored, secret
// and protected files are hidden until asked for; writing a secret or
// protected file needs an explicit confirm. A write carries the sha the
// person loaded, so it never overwrites what an agent changed meanwhile.

const (
	maxEditBytes    = 1 << 20  // larger files are shown as too large, not opened
	maxUploadBytes  = 10 << 20 // one uploaded file (the body limit is 15 MB of base64)
	maxDirItems     = 1000
	maxPreviewBytes = 100 << 20
)

func (s *server) fileRoutes(mux *http.ServeMux, admin func(http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/projects/{id}/files", admin(s.listFiles))
	mux.Handle("GET /api/projects/{id}/file", admin(s.readFile))
	mux.Handle("GET /api/projects/{id}/file/preview", admin(s.previewFile))
	mux.Handle("PUT /api/projects/{id}/file", admin(s.writeFile))
	mux.Handle("GET /api/projects/{id}/file/diff", admin(s.fileDiff))
	mux.Handle("POST /api/projects/{id}/file/upload", admin(s.uploadFile))
}

type fileFlags struct {
	Ignored   bool `json:"ignored"`
	Secret    bool `json:"secret"`
	Protected bool `json:"protected"`
}

type fileEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size"`
	fileFlags
}

// fileTarget is a project's folder and one cleaned relative path in it.
type fileTarget struct {
	root, rel, full string
	policy          perm.Policy
}

func (s *server) fileTarget(w http.ResponseWriter, r *http.Request, rel string) (fileTarget, bool) {
	p, ok, err := s.projectRoot(r, r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, r, err)
		return fileTarget{}, false
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "project chưa có thư mục")
		return fileTarget{}, false
	}
	rel = path.Clean("/" + filepath.ToSlash(strings.TrimSpace(rel)))[1:] // "" is the root
	if rel == ".git" || strings.HasPrefix(rel, ".git/") {
		writeError(w, http.StatusForbidden, "không mở thư mục .git")
		return fileTarget{}, false
	}
	full, err := chat.Workspace{Root: p.Path}.Resolve(cmpRel(rel))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return fileTarget{}, false
	}
	return fileTarget{root: p.Path, rel: rel, full: full, policy: perm.LoadPolicy(r.Context(), s.cfg.Store, r.PathValue("id"))}, true
}

func cmpRel(rel string) string {
	if rel == "" {
		return "."
	}
	return rel
}

func (t fileTarget) flags(rel string, dir bool, ignored map[string]bool) fileFlags {
	key := rel
	if dir {
		key += "/"
	}
	return fileFlags{
		Ignored:   ignored[key],
		Secret:    !dir && chat.IsSecret(path.Base(rel)),
		Protected: len(t.policy.Denied([]string{key})) > 0,
	}
}

// listFiles lists one folder level; hidden=1 also shows ignored and secret entries.
func (s *server) listFiles(w http.ResponseWriter, r *http.Request) {
	t, ok := s.fileTarget(w, r, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	hidden := r.URL.Query().Get("hidden") == "1"
	items, err := os.ReadDir(t.full)
	if err != nil {
		s.fileError(w, r, err)
		return
	}
	out := make([]fileEntry, 0, len(items))
	var check []string
	for _, e := range items {
		if t.rel == "" && e.Name() == ".git" {
			continue
		}
		rel := path.Join(t.rel, e.Name())
		dir := e.IsDir()
		if e.Type()&fs.ModeSymlink != 0 { // a link to a folder opens like one
			if st, err := os.Stat(filepath.Join(t.full, e.Name())); err == nil {
				dir = st.IsDir()
			}
		}
		var size int64
		if info, err := e.Info(); err == nil && !dir {
			size = info.Size()
		}
		out = append(out, fileEntry{Name: e.Name(), Path: rel, Dir: dir, Size: size})
		if dir {
			check = append(check, rel+"/")
		} else {
			check = append(check, rel)
		}
	}
	ignored := gitops.Ignored(r.Context(), t.root, check)
	shown := out[:0]
	hiddenCount := 0
	for _, e := range out {
		e.fileFlags = t.flags(e.Path, e.Dir, ignored)
		if !hidden && (e.Ignored || e.Secret) {
			hiddenCount++
			continue
		}
		shown = append(shown, e)
	}
	sort.SliceStable(shown, func(i, j int) bool {
		if shown[i].Dir != shown[j].Dir {
			return shown[i].Dir
		}
		return strings.ToLower(shown[i].Name) < strings.ToLower(shown[j].Name)
	})
	more := 0
	if len(shown) > maxDirItems {
		more = len(shown) - maxDirItems
		shown = shown[:maxDirItems]
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": t.rel, "entries": shown, "hidden": hiddenCount, "more": more})
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func isBinary(b []byte) bool {
	head := b
	if len(head) > 8000 {
		head = head[:8000]
	}
	return bytes.IndexByte(head, 0) >= 0 || !utf8.Valid(b)
}

type filePreview struct {
	Kind string `json:"kind"`
	Mime string `json:"mime"`
}

// Only formats the browser can display without executing project HTML or JS.
func previewType(name string) (filePreview, bool) {
	types := map[string]filePreview{
		".png": {"image", "image/png"}, ".jpg": {"image", "image/jpeg"}, ".jpeg": {"image", "image/jpeg"},
		".gif": {"image", "image/gif"}, ".webp": {"image", "image/webp"}, ".avif": {"image", "image/avif"},
		".bmp": {"image", "image/bmp"}, ".ico": {"image", "image/x-icon"}, ".svg": {"image", "image/svg+xml"},
		".pdf":  {"pdf", "application/pdf"},
		".html": {"document", "text/html; charset=utf-8"}, ".htm": {"document", "text/html; charset=utf-8"},
		".mp3": {"audio", "audio/mpeg"}, ".wav": {"audio", "audio/wav"}, ".ogg": {"audio", "audio/ogg"},
		".oga": {"audio", "audio/ogg"}, ".m4a": {"audio", "audio/mp4"}, ".aac": {"audio", "audio/aac"},
		".flac": {"audio", "audio/flac"}, ".opus": {"audio", "audio/ogg"},
		".mp4": {"video", "video/mp4"}, ".m4v": {"video", "video/mp4"}, ".webm": {"video", "video/webm"},
		".ogv": {"video", "video/ogg"}, ".mov": {"video", "video/quicktime"},
	}
	p, ok := types[strings.ToLower(filepath.Ext(name))]
	return p, ok
}

// readFile returns a text file's content and its sha (the version a save is based on).
func (s *server) readFile(w http.ResponseWriter, r *http.Request) {
	t, ok := s.fileTarget(w, r, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	st, err := os.Stat(t.full)
	if err != nil {
		s.fileError(w, r, err)
		return
	}
	if st.IsDir() {
		writeError(w, http.StatusBadRequest, "đây là thư mục")
		return
	}
	res := map[string]any{"path": t.rel, "size": st.Size(), "flags": t.flags(t.rel, false, gitops.Ignored(r.Context(), t.root, []string{t.rel}))}
	if p, ok := previewType(t.rel); ok && st.Size() <= maxPreviewBytes {
		res["preview"] = p
	}
	if st.Size() > maxEditBytes {
		res["too_large"] = true
		writeJSON(w, http.StatusOK, res)
		return
	}
	b, err := os.ReadFile(t.full)
	if err != nil {
		s.fileError(w, r, err)
		return
	}
	res["sha"] = sha(b)
	if isBinary(b) {
		res["binary"] = true
	} else {
		res["content"] = string(b)
	}
	writeJSON(w, http.StatusOK, res)
}

// previewFile streams a bounded, allowlisted format to the browser. Content
// is sandboxed because project files, including SVG and PDF, are untrusted.
func (s *server) previewFile(w http.ResponseWriter, r *http.Request) {
	t, ok := s.fileTarget(w, r, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	p, ok := previewType(t.rel)
	if !ok {
		writeError(w, http.StatusUnsupportedMediaType, "định dạng không hỗ trợ xem trước")
		return
	}
	f, err := os.Open(t.full)
	if err != nil {
		s.fileError(w, r, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		s.fileError(w, r, err)
		return
	}
	if st.IsDir() {
		writeError(w, http.StatusBadRequest, "đây là thư mục")
		return
	}
	if st.Size() > maxPreviewBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file quá lớn để xem trước")
		return
	}
	h := w.Header()
	h.Set("Content-Type", p.Mime)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src data:; media-src 'self'; style-src 'unsafe-inline'; form-action 'none'; base-uri 'none'")
	h.Set("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(filepath.Base(t.rel)))
	h.Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// writeFile saves (or creates, with sha "") a text file. A sha that no longer
// matches the file is a conflict: someone changed it after it was loaded.
func (s *server) writeFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4*maxEditBytes)
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Sha     string `json:"sha"`
		Confirm bool   `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	t, ok := s.fileTarget(w, r, in.Path)
	if !ok {
		return
	}
	if t.rel == "" {
		writeError(w, http.StatusBadRequest, "thiếu đường dẫn file")
		return
	}
	if len(in.Content) > maxEditBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file quá lớn để sửa trên dashboard")
		return
	}
	f := t.flags(t.rel, false, nil)
	if (f.Secret || f.Protected) && !in.Confirm {
		writeError(w, http.StatusPreconditionRequired, "file bí mật hoặc được bảo vệ: cần xác nhận trước khi lưu")
		return
	}
	// a new file: its folder must resolve inside the project too (no symlink out)
	if _, err := (chat.Workspace{Root: t.root}).Resolve(path.Dir(t.rel)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	mode := fs.FileMode(0o644)
	cur, err := os.ReadFile(t.full)
	created := errors.Is(err, os.ErrNotExist)
	switch {
	case err == nil:
		if st, err := os.Stat(t.full); err == nil {
			mode = st.Mode().Perm()
		}
		if sha(cur) != in.Sha {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "file đã bị thay đổi sau khi mở; tải lại rồi sửa tiếp", "sha": sha(cur)})
			return
		}
	case created:
		if in.Sha != "" {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "file đã bị xóa sau khi mở", "sha": ""})
			return
		}
		if err := os.MkdirAll(filepath.Dir(t.full), 0o755); err != nil {
			s.fileError(w, r, err)
			return
		}
	default:
		s.fileError(w, r, err)
		return
	}
	if err := os.WriteFile(t.full, []byte(in.Content), mode); err != nil {
		s.fileError(w, r, err)
		return
	}
	s.audit(r, audit.Change{Action: "file.write", ResourceID: t.rel, ProjectID: r.PathValue("id"),
		Detail: map[string]any{"bytes": len(in.Content), "created": created, "secret": f.Secret, "protected": f.Protected}})
	writeJSON(w, http.StatusOK, map[string]any{"path": t.rel, "sha": sha([]byte(in.Content))})
}

// uploadFile puts one uploaded file (any content, base64) into the project;
// a folder upload is one call per file. An existing file is replaced only
// with overwrite (409 otherwise), a secret or protected one only with confirm.
func (s *server) uploadFile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path      string `json:"path"`
		Data      string `json:"data"`
		Overwrite bool   `json:"overwrite"`
		Confirm   bool   `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	t, ok := s.fileTarget(w, r, in.Path)
	if !ok {
		return
	}
	if t.rel == "" {
		writeError(w, http.StatusBadRequest, "thiếu đường dẫn file")
		return
	}
	data, err := base64.StdEncoding.DecodeString(in.Data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "dữ liệu file không hợp lệ")
		return
	}
	if len(data) > maxUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file quá lớn (tối đa 10 MB)")
		return
	}
	f := t.flags(t.rel, false, nil)
	if (f.Secret || f.Protected) && !in.Confirm {
		writeError(w, http.StatusPreconditionRequired, "file bí mật hoặc được bảo vệ: cần xác nhận trước khi ghi")
		return
	}
	if _, err := (chat.Workspace{Root: t.root}).Resolve(path.Dir(t.rel)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	mode := fs.FileMode(0o644)
	st, err := os.Stat(t.full)
	created := errors.Is(err, os.ErrNotExist)
	switch {
	case err == nil:
		if st.IsDir() {
			writeError(w, http.StatusConflict, "đã có thư mục cùng tên")
			return
		}
		if !in.Overwrite {
			writeError(w, http.StatusConflict, "file đã tồn tại")
			return
		}
		mode = st.Mode().Perm()
	case created:
		if err := os.MkdirAll(filepath.Dir(t.full), 0o755); err != nil {
			s.fileError(w, r, err)
			return
		}
	default:
		s.fileError(w, r, err)
		return
	}
	if err := os.WriteFile(t.full, data, mode); err != nil {
		s.fileError(w, r, err)
		return
	}
	s.audit(r, audit.Change{Action: "file.upload", ResourceID: t.rel, ProjectID: r.PathValue("id"),
		Detail: map[string]any{"bytes": len(data), "created": created, "secret": f.Secret, "protected": f.Protected}})
	writeJSON(w, http.StatusOK, map[string]any{"path": t.rel, "sha": sha(data)})
}

// fileDiff is the file's uncommitted change against HEAD.
func (s *server) fileDiff(w http.ResponseWriter, r *http.Request) {
	t, ok := s.fileTarget(w, r, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	d, err := gitops.Diff(r.Context(), t.root, []string{cmpRel(t.rel)}, 400_000)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if d == "" && t.rel != "" && !gitops.Tracked(r.Context(), t.root, t.rel) {
		d = newFileDiff(t.rel, t.full, 400_000) // a new file: all of it added
	}
	writeJSON(w, http.StatusOK, map[string]any{"diff": d})
}

// newFileDiff is an untracked file as a diff that adds it ("" when it cannot
// be read as text).
func newFileDiff(rel, full string, max int) string {
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		return ""
	}
	f, err := os.Open(full)
	if err != nil {
		return ""
	}
	defer f.Close()
	data := make([]byte, min(info.Size(), int64(max)))
	n, _ := io.ReadFull(f, data)
	data = data[:n]
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "--- /dev/null\n+++ b/" + rel + "\n(file nhị phân)"
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", rel, len(lines))
	for _, l := range lines {
		b.WriteString("+" + l + "\n")
	}
	if info.Size() > int64(max) {
		b.WriteString("… (cắt bớt)\n")
	}
	return b.String()
}

func (s *server) fileError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, os.ErrNotExist):
		writeError(w, http.StatusNotFound, "không tìm thấy file")
	case errors.Is(err, os.ErrPermission):
		writeError(w, http.StatusForbidden, "không có quyền với file này")
	default:
		s.internal(w, r, err)
	}
}
