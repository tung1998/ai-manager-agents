package ops

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// EnvFile is a config file of the project (.env, app.properties,
// settings.yaml, appsettings.json, web.config…), for the "Môi trường"
// section to edit as a list of keys and values.
type EnvFile struct {
	Path    string `json:"path"`   // relative to the project, slash-separated
	Format  string `json:"format"` // env | properties | ini | toml | yaml | json | xml
	Size    int64  `json:"size"`
	Example bool   `json:"example"` // a template (.env.example, config.sample.yaml…): no secrets
}

const (
	envFilesMax   = 300
	envFilesDepth = 5
	envFileBytes  = 1 << 20 // larger ones are data, not config
)

// folders that hold no config of the project's own: dependencies, builds,
// and every hidden one (.git, CI, editors and agents, office's worktrees)
var envSkipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "venv": true, "__pycache__": true, "dist": true, "build": true,
	"target": true, "coverage": true, "bin": true, "obj": true, "testdata": true, "fixtures": true,
	"locales": true, "i18n": true, "lang": true,
}

var (
	// yaml/json/xml are config only by their name or folder: most are data
	configNameRe = regexp.MustCompile(`(?i)(config|conf|settings|appsettings|application|secrets?|parameters|credentials|env|properties)`)
	configDirRe  = regexp.MustCompile(`(?i)^(config|configs|conf|settings|env|environments|deploy|k8s|helm)$`)
	// tool configs, lockfiles, manifests: not the app's settings
	notConfigRe = regexp.MustCompile(`(?i)(^tsconfig|^jsconfig|lock\.(json|yaml)$|^package(-lock)?\.json$|^composer\.json$|^\.?eslint|^\.?prettier|^\.?stylelint|^renovate|^lerna|^nx\.json$|^turbo\.json$|^\.markdownlint|^components\.json$|\.schema\.json$)`)
)

// ConfigFormat is how a file name reads as config ("" when it does not).
func ConfigFormat(dir, name string) string {
	lower := strings.ToLower(name)
	switch {
	case lower == ".env" || strings.HasPrefix(lower, ".env.") || (strings.HasSuffix(lower, ".env") && len(lower) > 4) || lower == ".flaskenv":
		return "env"
	case notConfigRe.MatchString(lower):
		return ""
	}
	ext := strings.TrimPrefix(filepath.Ext(lower), ".")
	if isEnvExample(lower) { // config.yaml.example: the format of the name before
		ext = strings.TrimPrefix(filepath.Ext(strings.TrimSuffix(lower, "."+ext)), ".")
	}
	byName := configNameRe.MatchString(strings.TrimSuffix(lower, filepath.Ext(lower))) || configDirRe.MatchString(filepath.Base(dir))
	switch ext {
	case "properties":
		return "properties"
	case "ini", "cfg", "conf", "cnf":
		return "ini"
	case "toml":
		if lower == "cargo.toml" || lower == "pyproject.toml" {
			return ""
		}
		return "toml"
	case "yaml", "yml":
		if byName && !strings.Contains(lower, "docker-compose") && !strings.HasPrefix(lower, "compose.") {
			return "yaml"
		}
	case "json":
		if byName {
			return "json"
		}
	case "xml", "config":
		if ext == "config" || byName {
			return "xml"
		}
	}
	return ""
}

func isEnvExample(name string) bool {
	for _, s := range []string{"example", "sample", "template", ".dist", "defaults"} {
		if strings.Contains(strings.ToLower(name), s) {
			return true
		}
	}
	return false
}

// FindEnvFiles lists the config files under root (not following symlinks,
// not in dependency or build folders): .env ones first, then the shallowest.
func FindEnvFiles(root string) []EnvFile {
	out := []EnvFile{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			if rel != "." && (envSkipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") || strings.Count(filepath.ToSlash(rel), "/") >= envFilesDepth-1) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		format := ConfigFormat(filepath.Dir(rel), d.Name())
		if format == "" {
			return nil
		}
		f := EnvFile{Path: filepath.ToSlash(rel), Format: format, Example: isEnvExample(d.Name())}
		if info, err := d.Info(); err == nil {
			if info.Size() > envFileBytes {
				return nil
			}
			f.Size = info.Size()
		}
		out = append(out, f)
		if len(out) >= envFilesMax {
			return fs.SkipAll
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool {
		if ei, ej := out[i].Format == "env", out[j].Format == "env"; ei != ej {
			return ei
		}
		di, dj := strings.Count(out[i].Path, "/"), strings.Count(out[j].Path, "/")
		if di != dj {
			return di < dj
		}
		return out[i].Path < out[j].Path
	})
	return out
}
