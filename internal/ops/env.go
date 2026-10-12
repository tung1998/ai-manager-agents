package ops

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// EnvHint is a setting a command runs with, found without running it.
type EnvHint struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"`         // command | script | env-file | default
	File   string `json:"file,omitempty"` // the package.json or .env it came from (relative to the project)
	Tool   string `json:"tool,omitempty"` // default: the framework (vite, nuxt…)
	// Flag: the text in the command that holds it ("--port 3000"): changing
	// the value changes the command. Append: when it is not in the command
	// and the tool reads no variable, what to add to it ("%s" = the value).
	Flag   string `json:"flag,omitempty"`
	Append string `json:"append,omitempty"`
}

var (
	// --port 3000, --port=3000, -p 3000 (a short -p only when it reads as a port)
	portFlagRe = regexp.MustCompile(`(?:^|\s)((--port|--http-port|-p)(?:=|\s+)(\d{2,5}))\b`)
	// PORT=3000 before a command
	portAssignRe = regexp.MustCompile(`(?:^|[\s;&(])((PORT)=(\d{2,5}))\b`)
	// what a .env may say about where it listens (no URLs: they hold passwords)
	envKeyRe = regexp.MustCompile(`^(PORT|HOST|HOSTNAME|[A-Z0-9_]*_PORT)$`)
)

// a framework's port when nothing sets it: key is the variable it reads ("":
// none, the port goes in the command as arg)
var toolDefaults = []struct {
	re         *regexp.Regexp
	tool, port string
	key, arg   string
}{
	{regexp.MustCompile(`\bvite\s+preview\b`), "vite", "4173", "", "--port"},
	{regexp.MustCompile(`\bvite(\s+(dev|serve)\b|\s*$|\s+--)`), "vite", "5173", "", "--port"},
	{regexp.MustCompile(`\bnux[ti]\s+(dev|preview)\b`), "nuxt", "3000", "PORT", ""},
	{regexp.MustCompile(`\bnext\s+(dev|start)\b`), "next", "3000", "PORT", ""},
	{regexp.MustCompile(`\breact-scripts\s+start\b`), "react-scripts", "3000", "PORT", ""},
	{regexp.MustCompile(`\bastro\s+(dev|preview)\b`), "astro", "4321", "", "--port"},
	{regexp.MustCompile(`\bng\s+serve\b`), "angular", "4200", "", "--port"},
	{regexp.MustCompile(`\bwrangler\s+dev\b`), "wrangler", "8787", "", "--port"},
	{regexp.MustCompile(`\bhugo\s+server\b`), "hugo", "1313", "", "--port"},
	{regexp.MustCompile(`\brails\s+(s|server)\b`), "rails", "3000", "PORT", ""},
	{regexp.MustCompile(`\bflask\s+run\b`), "flask", "5000", "FLASK_RUN_PORT", ""},
	{regexp.MustCompile(`\buvicorn\b`), "uvicorn", "8000", "UVICORN_PORT", ""},
}

// DetectEnv reads what command (run in cwd, relative to root) would listen
// on: a port in the command itself, else in the package.json script it runs,
// else in the .env of its folder, else its framework's default. Plus the
// HOST/…_PORT a .env sets. Nothing is run.
func DetectEnv(root, cwd, command string) []EnvHint {
	out := []EnvHint{}
	have := map[string]bool{}
	add := func(h EnvHint) {
		if !have[h.Key] {
			have[h.Key] = true
			out = append(out, h)
		}
	}
	command = strings.TrimSpace(command)
	if h, ok := portIn(command); ok {
		h.Source = "command"
		add(h)
	}
	script, pkg := scriptOf(root, cwd, command)
	if h, ok := portIn(script); ok {
		h.Source, h.File, h.Flag = "script", pkg, "" // not in the command: it cannot be changed there
		add(h)
	}
	for _, h := range envFiles(root, cwd) {
		add(h)
	}
	if !have["PORT"] {
		text := command + "\n" + script
		for _, d := range toolDefaults {
			if !d.re.MatchString(text) {
				continue
			}
			h := EnvHint{Key: "PORT", Value: d.port, Source: "default", Tool: d.tool}
			if d.key != "" {
				h.Key = d.key
			} else {
				h.Append = " " + d.arg + " %s"
				if script != "" && strings.HasPrefix(command, "npm ") && !strings.Contains(command, " -- ") {
					h.Append = " --" + h.Append // npm passes on what comes after --
				}
			}
			if !have[h.Key] {
				add(h)
			}
			break
		}
	}
	return out
}

// portIn finds a port the text sets, by flag or by PORT=.
func portIn(text string) (EnvHint, bool) {
	for _, m := range portFlagRe.FindAllStringSubmatch(text, -1) {
		n, _ := strconv.Atoi(m[3])
		if n > 65535 || (m[2] == "-p" && n < 1000) { // `go test -p 4`, `-p 10`: not a port
			continue
		}
		return EnvHint{Key: "PORT", Value: m[3], Flag: m[1]}, true
	}
	if m := portAssignRe.FindStringSubmatch(text); m != nil {
		return EnvHint{Key: "PORT", Value: m[3], Flag: m[1]}, true
	}
	return EnvHint{}, false
}

// scriptOf: the package.json script a command runs (npm run dev, pnpm dev,
// yarn dev, bun run dev), and that package.json (relative to root).
func scriptOf(root, cwd, command string) (string, string) {
	f := strings.Fields(command)
	if len(f) < 2 {
		return "", ""
	}
	name := ""
	switch f[0] {
	case "npm":
		if f[1] == "start" || f[1] == "test" {
			name = f[1]
		} else if (f[1] == "run" || f[1] == "run-script") && len(f) > 2 {
			name = f[2]
		}
	case "pnpm", "yarn", "bun":
		name = f[1]
		if f[1] == "run" && len(f) > 2 {
			name = f[2]
		}
	}
	if name == "" {
		return "", ""
	}
	rel := filepath.Join(cwd, "package.json")
	return readPackage(filepath.Join(root, rel))[name], rel
}

// envFiles: PORT, HOST and …_PORT from the .env files of the folder, the
// one a dev server would read first winning.
func envFiles(root, cwd string) []EnvHint {
	out := []EnvHint{}
	seen := map[string]bool{}
	for _, name := range []string{".env.development.local", ".env.local", ".env.development", ".env", ".env.example"} {
		rel := filepath.Join(cwd, name)
		f, err := os.Open(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(sc.Text()), "export "))
			k, v, ok := strings.Cut(line, "=")
			k = strings.TrimSpace(k)
			if !ok || strings.HasPrefix(k, "#") || !envKeyRe.MatchString(k) || seen[k] {
				continue
			}
			v = strings.TrimSpace(v)
			if i := strings.Index(v, " #"); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
			v = strings.Trim(v, `"'`)
			if v == "" {
				continue
			}
			seen[k] = true
			out = append(out, EnvHint{Key: k, Value: v, Source: "env-file", File: rel})
		}
		f.Close()
	}
	return out
}
