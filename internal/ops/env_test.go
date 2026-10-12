package ops

import (
	"os"
	"path/filepath"
	"testing"
)

func hintOf(hs []EnvHint, key string) (EnvHint, bool) {
	for _, h := range hs {
		if h.Key == key {
			return h, true
		}
	}
	return EnvHint{}, false
}

// The port comes from the command, else its script, else the folder's .env,
// else the framework's default; a .env's HOST and …_PORT come along, its
// URLs (passwords) never.
func TestDetectEnv(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "web"), 0o755)
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"dev":"nuxt dev","pinned":"vite --port 4000","plain":"vite","api":"node server.js"}}`), 0o644)
	os.WriteFile(filepath.Join(root, "web/package.json"), []byte(`{"scripts":{"dev":"vite"}}`), 0o644)
	os.WriteFile(filepath.Join(root, ".env"), []byte("# local\nexport HOST=0.0.0.0\nDB_PORT=5432 # pg\nDATABASE_URL=postgres://u:secret@x/db\nAPI_URL=http://localhost:9000\n"), 0o644)

	cases := []struct {
		cwd, cmd           string
		key, value, source string
		flag, append       string
	}{
		{".", "vite --port 3001", "PORT", "3001", "command", "--port 3001", ""},
		{".", "PORT=8080 node server.js", "PORT", "8080", "command", "PORT=8080", ""},
		{".", "pnpm pinned", "PORT", "4000", "script", "", ""},
		{".", "pnpm dev", "PORT", "3000", "default", "", ""},
		{".", "pnpm plain", "PORT", "5173", "default", "", " --port %s"},
		{".", "npm run plain", "PORT", "5173", "default", "", " -- --port %s"},
		{"web", "pnpm dev", "PORT", "5173", "default", "", " --port %s"},
		{".", "flask run", "FLASK_RUN_PORT", "5000", "default", "", ""},
	}
	for _, c := range cases {
		h, ok := hintOf(DetectEnv(root, c.cwd, c.cmd), c.key)
		if !ok || h.Value != c.value || h.Source != c.source || h.Flag != c.flag || h.Append != c.append {
			t.Errorf("%s (%s): %s = %+v", c.cmd, c.cwd, c.key, h)
		}
	}
	hs := DetectEnv(root, ".", "pnpm api")
	if _, ok := hintOf(hs, "PORT"); ok {
		t.Errorf("a port out of nothing: %+v", hs)
	}
	if h, _ := hintOf(hs, "HOST"); h.Value != "0.0.0.0" || h.Source != "env-file" || h.File != ".env" {
		t.Errorf("HOST: %+v", h)
	}
	if h, _ := hintOf(hs, "DB_PORT"); h.Value != "5432" {
		t.Errorf("DB_PORT: %+v", h)
	}
	for _, k := range []string{"DATABASE_URL", "API_URL"} {
		if _, ok := hintOf(hs, k); ok {
			t.Errorf("%s read from .env", k)
		}
	}
	for _, cmd := range []string{"go test -p 4 ./...", "mkdir -p 10 && ls", "make build"} {
		if h, ok := hintOf(DetectEnv(root, "web", cmd), "PORT"); ok {
			t.Errorf("%s: %+v", cmd, h)
		}
	}
}
