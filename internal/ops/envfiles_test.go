package ops

import (
	"os"
	"path/filepath"
	"testing"
)

// config files anywhere in the project, not in dependencies, builds or CI;
// .env first, then the shallowest; yaml/json/xml only by a config name or
// folder; templates are marked.
func TestFindEnvFiles(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{
		".env", ".env.example", "api/.env.local", "api/app.env", "node_modules/x/.env", "dist/.env", ".office/worktrees/w/.env",
		"README.md", "a/environment.go", "package.json", "tsconfig.json", "pnpm-lock.yaml", "docker-compose.yml", ".github/workflows/ci.yml",
		"data/users.json", "src/main/resources/application.properties", "config/database.yml", "appsettings.json", "web.config",
		"setup.cfg", "app/settings.toml", "config.yaml.example", "pyproject.toml", ".claude/settings.json", "schema/app.config.schema.json",
	} {
		p := filepath.Join(dir, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("K=1\n"), 0o644)
	}
	got := FindEnvFiles(dir)
	want := map[string]string{
		".env": "env", ".env.example": "env", "api/.env.local": "env", "api/app.env": "env",
		"appsettings.json": "json", "web.config": "xml", "setup.cfg": "ini", "config.yaml.example": "yaml",
		"app/settings.toml": "toml", "config/database.yml": "yaml", "src/main/resources/application.properties": "properties",
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, f := range got {
		if want[f.Path] != f.Format {
			t.Errorf("%s: format %q, want %q", f.Path, f.Format, want[f.Path])
		}
		if i < 4 && f.Format != "env" {
			t.Errorf(".env files first: %+v", got)
		}
	}
	if got[0].Path != ".env" || got[0].Example || !got[1].Example || got[0].Size != 4 {
		t.Fatalf("order/flags: %+v", got[:2])
	}
}
