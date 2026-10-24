// Package target reads downstream.yml, the small file that tells the engine how to
// build and start the app under test. The file is read from the base commit, so a
// change under review cannot alter how it is measured.
package target

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Config is the parsed downstream.yml.
type Config struct {
	Root       string // directory scanned for routes, relative to the repo
	MainDir    string // optional: directory of the app's package main, relative to the repo; enables the runtime counters
	Build      string // shell command; {out} is the binary path
	Start      string // shell command; {out} is the binary path
	PortEnv    string // environment variable that carries the loopback port
	ReadyPath  string // path polled until the app answers
	VUs        int
	Iterations int           // journey iterations per VU; a fixed sample size per pass
	Duration   time.Duration // cap on the time one pass may take
	Frontend   Frontend
}

// Frontend describes a static frontend build. All fields are empty when the
// repo declares none; the frontend stage then checks the pages the backend
// itself serves.
type Frontend struct {
	Dir   string // directory holding package.json, relative to the repo; "." by default
	Build string // shell command run in Dir, such as "npm run build"
	Out   string // directory the build writes, relative to the repo
	Node  string // exact Node version, such as 20.20.2; the PATH's node when empty
}

// HasBackend reports whether the file declares a backend to build and run.
func (c Config) HasBackend() bool { return c.Build != "" }

// HasStaticFrontend reports whether the file declares a frontend build.
func (c Config) HasStaticFrontend() bool { return c.Frontend.Out != "" }

const (
	defaultVUs        = 2
	defaultIterations = 20
	defaultDuration   = 60 * time.Second
	minVUs, maxVUs    = 1, 10
	minIter, maxIter  = 1, 500
	minDuration       = 10 * time.Second
	maxDuration       = 170 * time.Second
)

var (
	keyRe     = regexp.MustCompile(`^[a-z_]+$`)
	portEnvRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	nodeVerRe = regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$`)
)

var backendKeys = []string{"root", "build", "start", "port_env", "ready_path"}

// Parse reads the flat subset of YAML the engine supports: "key: value" lines,
// "#" comments, optional single or double quotes. Anything else is an error that
// names the line.
func Parse(data []byte) (Config, error) {
	var cfg Config
	seen := map[string]int{}
	vus, iterations, duration := defaultVUs, defaultIterations, defaultDuration

	for i, raw := range strings.Split(string(data), "\n") {
		n := i + 1
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			return Config{}, fmt.Errorf("line %d: indentation and nesting are not supported", n)
		}
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			return Config{}, fmt.Errorf("line %d: expected \"key: value\"", n)
		}
		if !keyRe.MatchString(key) {
			return Config{}, fmt.Errorf("line %d: invalid key %q", n, key)
		}
		if prev, dup := seen[key]; dup {
			return Config{}, fmt.Errorf("line %d: key %q already set on line %d", n, key, prev)
		}
		val, err := parseValue(rest)
		if err != nil {
			return Config{}, fmt.Errorf("line %d: %s: %w", n, key, err)
		}
		seen[key] = n

		switch key {
		case "root":
			cfg.Root = val
		case "main_dir":
			cfg.MainDir = val
		case "build":
			cfg.Build = val
		case "start":
			cfg.Start = val
		case "port_env":
			cfg.PortEnv = val
		case "ready_path":
			cfg.ReadyPath = val
		case "frontend_dir":
			cfg.Frontend.Dir = val
		case "frontend_build":
			cfg.Frontend.Build = val
		case "frontend_out":
			cfg.Frontend.Out = val
		case "frontend_node":
			cfg.Frontend.Node = val
		case "vus":
			v, err := strconv.Atoi(val)
			if err != nil {
				return Config{}, fmt.Errorf("line %d: vus: %q is not an integer", n, val)
			}
			if v < minVUs || v > maxVUs {
				return Config{}, fmt.Errorf("line %d: vus: %d is outside %d..%d", n, v, minVUs, maxVUs)
			}
			vus = v
		case "iterations":
			v, err := strconv.Atoi(val)
			if err != nil {
				return Config{}, fmt.Errorf("line %d: iterations: %q is not an integer", n, val)
			}
			if v < minIter || v > maxIter {
				return Config{}, fmt.Errorf("line %d: iterations: %d is outside %d..%d", n, v, minIter, maxIter)
			}
			iterations = v
		case "duration":
			d, err := time.ParseDuration(val)
			if err != nil {
				return Config{}, fmt.Errorf("line %d: duration: %q is not a duration such as 15s", n, val)
			}
			if d < minDuration || d > maxDuration {
				return Config{}, fmt.Errorf("line %d: duration: %s is outside %s..%s", n, d, minDuration, maxDuration)
			}
			duration = d
		default:
			return Config{}, fmt.Errorf("line %d: unknown key %q", n, key)
		}
	}

	if err := checkFrontend(&cfg.Frontend, seen); err != nil {
		return Config{}, err
	}
	// A frontend-only repo has no backend keys at all. Once any backend key is
	// present, all of them are required.
	backend := cfg.Frontend.Out == ""
	for _, k := range backendKeys {
		if _, ok := seen[k]; ok {
			backend = true
		}
	}
	if !backend {
		cfg.VUs, cfg.Iterations, cfg.Duration = vus, iterations, duration
		return cfg, nil
	}
	for _, req := range []struct{ key, val string }{
		{"root", cfg.Root}, {"build", cfg.Build}, {"start", cfg.Start},
		{"port_env", cfg.PortEnv}, {"ready_path", cfg.ReadyPath},
	} {
		if _, ok := seen[req.key]; !ok {
			return Config{}, fmt.Errorf("missing required key %q", req.key)
		}
		if req.val == "" {
			return Config{}, fmt.Errorf("line %d: %s must not be empty", seen[req.key], req.key)
		}
	}
	if strings.HasPrefix(cfg.Root, "/") || strings.Contains(cfg.Root, "..") {
		return Config{}, fmt.Errorf("line %d: root must be a relative path inside the repo", seen["root"])
	}
	if n, ok := seen["main_dir"]; ok {
		if cfg.MainDir == "" {
			return Config{}, fmt.Errorf("line %d: main_dir must not be empty", n)
		}
		if strings.HasPrefix(cfg.MainDir, "/") || strings.Contains(cfg.MainDir, "..") {
			return Config{}, fmt.Errorf("line %d: main_dir must be a relative path inside the repo", n)
		}
	}
	if !portEnvRe.MatchString(cfg.PortEnv) {
		return Config{}, fmt.Errorf("line %d: port_env %q is not an environment variable name", seen["port_env"], cfg.PortEnv)
	}
	if !strings.HasPrefix(cfg.ReadyPath, "/") {
		return Config{}, fmt.Errorf("line %d: ready_path must start with /", seen["ready_path"])
	}
	cfg.VUs, cfg.Iterations, cfg.Duration = vus, iterations, duration
	if cfg.MainDir == "" {
		cfg.MainDir = mainDirFromBuild(cfg.Build)
	}
	return cfg, nil
}

// checkFrontend validates the frontend_* keys: build and out come together, and
// every path stays inside the repo.
func checkFrontend(f *Frontend, seen map[string]int) error {
	_, hasBuild := seen["frontend_build"]
	_, hasOut := seen["frontend_out"]
	if hasBuild != hasOut {
		return fmt.Errorf("frontend_build and frontend_out must be set together")
	}
	if n, ok := seen["frontend_dir"]; ok && !hasOut {
		return fmt.Errorf("line %d: frontend_dir needs frontend_build and frontend_out", n)
	}
	if n, ok := seen["frontend_node"]; ok && !hasOut {
		return fmt.Errorf("line %d: frontend_node needs frontend_build and frontend_out", n)
	}
	if !hasOut {
		return nil
	}
	if f.Dir == "" {
		f.Dir = "."
	}
	for _, p := range []struct{ key, val string }{{"frontend_dir", f.Dir}, {"frontend_out", f.Out}} {
		if strings.HasPrefix(p.val, "/") || strings.Contains(p.val, "..") {
			return fmt.Errorf("line %d: %s must be a relative path inside the repo", seen[p.key], p.key)
		}
	}
	if f.Out == "." {
		return fmt.Errorf("line %d: frontend_out must be the build's output directory, not the repo root", seen["frontend_out"])
	}
	if n, ok := seen["frontend_node"]; ok && !nodeVerRe.MatchString(f.Node) {
		return fmt.Errorf("line %d: frontend_node %q is not an exact version such as 20.20.2", n, f.Node)
	}
	return nil
}

// mainDirFromBuild finds the package a plain `go build` command builds, such as
// examples/shelf/cmd/shelf in "go build -o {out} ./examples/shelf/cmd/shelf", so
// the runtime counters work without a main_dir line. It returns "" for any other
// command, for several packages, or for a pattern with "...".
func mainDirFromBuild(build string) string {
	f := strings.Fields(build)
	if len(f) < 3 || f[0] != "go" || f[1] != "build" {
		return ""
	}
	pkg := ""
	for i := 2; i < len(f); i++ {
		a := f[i]
		switch {
		case a == "-o":
			i++ // the output path
		case strings.HasPrefix(a, "-"):
		case strings.HasPrefix(a, "./") && pkg == "":
			pkg = a
		default:
			return ""
		}
	}
	pkg = strings.TrimSuffix(strings.TrimPrefix(pkg, "./"), "/")
	if pkg == "" || strings.Contains(pkg, "...") || strings.Contains(pkg, "..") || strings.ContainsAny(pkg, "{}$`'\"") {
		return ""
	}
	return pkg
}

// parseValue handles the text after the first colon: optional quotes, trailing
// comment, and a refusal of list and map syntax.
func parseValue(rest string) (string, error) {
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return "", fmt.Errorf("expected a space after the colon")
	}
	v := strings.TrimSpace(rest)
	if v == "" || v[0] == '#' {
		return "", fmt.Errorf("empty value")
	}
	if v[0] == '"' || v[0] == '\'' {
		q := v[0]
		end := strings.IndexByte(v[1:], q)
		if end < 0 {
			return "", fmt.Errorf("unterminated quote")
		}
		out := v[1 : 1+end]
		tail := strings.TrimSpace(v[2+end:])
		if tail != "" && tail[0] != '#' {
			return "", fmt.Errorf("unexpected text after the closing quote")
		}
		if tail != "" && v[2+end] != ' ' && v[2+end] != '\t' {
			return "", fmt.Errorf("comment must be separated by whitespace")
		}
		return out, nil
	}
	if v[0] == '[' || v[0] == '-' && (len(v) == 1 || v[1] == ' ') || v[0] == '|' || v[0] == '>' || v[0] == '&' || v[0] == '*' {
		return "", fmt.Errorf("lists, block scalars and anchors are not supported")
	}
	for i := 1; i < len(v); i++ {
		if v[i] == '#' && (v[i-1] == ' ' || v[i-1] == '\t') {
			v = strings.TrimSpace(v[:i])
			break
		}
	}
	return v, nil
}

// Load reads downstream.yml as it exists at commit in the repository at repo.
func Load(repo, commit string) (Config, error) {
	if commit == "" || strings.HasPrefix(commit, "-") {
		return Config{}, fmt.Errorf("invalid commit %q", commit)
	}
	cmd := exec.Command("git", "-C", repo, "show", commit+":downstream.yml")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return Config{}, fmt.Errorf("reading downstream.yml at %s: %w: %s", commit, err, strings.TrimSpace(stderr.String()))
	}
	cfg, err := Parse(stdout.Bytes())
	if err != nil {
		return Config{}, fmt.Errorf("downstream.yml at %s: %w", commit, err)
	}
	return cfg, nil
}

// BuildCommand returns the build command with {out} replaced by out as one
// quoted shell word.
func (c Config) BuildCommand(out string) string {
	return strings.ReplaceAll(c.Build, "{out}", shellQuote(out))
}

// StartCommand returns the start command with {out} replaced by out as one
// quoted shell word.
func (c Config) StartCommand(out string) string {
	return strings.ReplaceAll(c.Start, "{out}", shellQuote(out))
}

// shellQuote makes s a single word for sh, whatever characters it holds.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
