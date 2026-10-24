package target

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const good = `# Downstream target config.
root: examples/shelf                      # scanned for routes
build: go build -o {out} ./examples/shelf/cmd/shelf
start: "{out}"
port_env: PORT
ready_path: /health
vus: 2
iterations: 20 # per VU; fixed sample size per pass
duration: 60s # cap per pass
`

func TestParseGood(t *testing.T) {
	got, err := Parse([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		Root: "examples/shelf", MainDir: "examples/shelf/cmd/shelf", // worked out from the build command
		Build: "go build -o {out} ./examples/shelf/cmd/shelf",
		Start: "{out}", PortEnv: "PORT", ReadyPath: "/health", VUs: 2, Iterations: 20, Duration: 60 * time.Second,
	}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseTable(t *testing.T) {
	req := "root: r\nbuild: b {out}\nstart: '{out}'\nport_env: PORT\nready_path: /health\n"
	tests := []struct {
		name    string
		in      string
		wantErr string // substring; empty means success
		check   func(t *testing.T, c Config)
	}{
		{name: "defaults", in: req, check: func(t *testing.T, c Config) {
			if c.VUs != 2 || c.Iterations != 20 || c.Duration != 60*time.Second {
				t.Fatalf("defaults: %+v", c)
			}
		}},
		{name: "comments and blank lines", in: "# top\n\n" + req + "   # indented comment\nvus: 3 # three\n", check: func(t *testing.T, c Config) {
			if c.VUs != 3 {
				t.Fatalf("vus = %d", c.VUs)
			}
		}},
		{name: "hash inside quotes is kept", in: strings.Replace(req, "build: b {out}", `build: "go build -o {out} #x" # tail`, 1), check: func(t *testing.T, c Config) {
			if c.Build != "go build -o {out} #x" {
				t.Fatalf("build = %q", c.Build)
			}
		}},
		{name: "hash without space is kept", in: strings.Replace(req, "root: r", "root: a#b", 1), check: func(t *testing.T, c Config) {
			if c.Root != "a#b" {
				t.Fatalf("root = %q", c.Root)
			}
		}},
		{name: "single quotes", in: strings.Replace(req, "root: r", "root: 'examples/x'", 1), check: func(t *testing.T, c Config) {
			if c.Root != "examples/x" {
				t.Fatalf("root = %q", c.Root)
			}
		}},
		{name: "unknown key", in: req + "\nretries: 3\n", wantErr: "line 7: unknown key \"retries\""},
		{name: "missing key", in: "root: r\nbuild: b\nstart: s\nport_env: PORT\n", wantErr: `missing required key "ready_path"`},
		{name: "bad duration", in: req + "duration: soon\n", wantErr: "line 6: duration"},
		{name: "duration too short", in: req + "duration: 9s\n", wantErr: "outside"},
		{name: "duration too long", in: req + "duration: 171s\n", wantErr: "outside"},
		{name: "duration at upper bound", in: req + "duration: 170s\n", check: func(t *testing.T, c Config) {
			if c.Duration != 170*time.Second {
				t.Fatalf("duration = %s", c.Duration)
			}
		}},
		{name: "iterations set", in: req + "iterations: 500\n", check: func(t *testing.T, c Config) {
			if c.Iterations != 500 {
				t.Fatalf("iterations = %d", c.Iterations)
			}
		}},
		{name: "iterations zero", in: req + "iterations: 0\n", wantErr: "line 6: iterations"},
		{name: "iterations too many", in: req + "iterations: 501\n", wantErr: "outside"},
		{name: "iterations not a number", in: req + "iterations: many\n", wantErr: "not an integer"},
		{name: "vus zero", in: req + "vus: 0\n", wantErr: "line 6: vus"},
		{name: "vus too many", in: req + "vus: 11\n", wantErr: "outside"},
		{name: "vus not a number", in: req + "vus: two\n", wantErr: "not an integer"},
		{name: "list syntax", in: req + "setup:\n  - echo hi\n", wantErr: "line 6"},
		{name: "inline list", in: strings.Replace(req, "root: r", "root: [a, b]", 1), wantErr: "line 1"},
		{name: "dash item", in: "- a\n" + req, wantErr: "line 1"},
		{name: "nesting", in: req + "  nested: x\n", wantErr: "line 6: indentation"},
		{name: "duplicate key", in: req + "root: again\n", wantErr: "line 6: key \"root\" already set on line 1"},
		{name: "empty value", in: strings.Replace(req, "root: r", "root:", 1), wantErr: "line 1: root: empty value"},
		{name: "unterminated quote", in: strings.Replace(req, "root: r", `root: "r`, 1), wantErr: "unterminated"},
		{name: "text after quote", in: strings.Replace(req, "root: r", `root: "r" x`, 1), wantErr: "after the closing quote"},
		{name: "no colon", in: "root r\n" + req, wantErr: "line 1"},
		{name: "absolute root", in: strings.Replace(req, "root: r", "root: /etc", 1), wantErr: "relative"},
		{name: "root escapes repo", in: strings.Replace(req, "root: r", "root: ../x", 1), wantErr: "relative"},
		{name: "main_dir is optional and read", in: req + "main_dir: cmd/app\n", check: func(t *testing.T, c Config) {
			if c.MainDir != "cmd/app" {
				t.Fatalf("main_dir = %q", c.MainDir)
			}
		}},
		{name: "main_dir absent", in: req, check: func(t *testing.T, c Config) {
			if c.MainDir != "" {
				t.Fatalf("main_dir = %q", c.MainDir)
			}
		}},
		{name: "absolute main_dir", in: req + "main_dir: /etc\n", wantErr: "line 6: main_dir must be a relative path"},
		{name: "main_dir escapes repo", in: req + "main_dir: ../x\n", wantErr: "line 6: main_dir must be a relative path"},
		{name: "empty quoted main_dir", in: req + "main_dir: \"\"\n", wantErr: "line 6: main_dir must not be empty"},
		{name: "bad port_env", in: strings.Replace(req, "PORT", "p;rt", 1), wantErr: "port_env"},
		{name: "ready_path without slash", in: strings.Replace(req, "/health", "health", 1), wantErr: "ready_path"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Parse([]byte(tc.in))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.check != nil {
				tc.check(t, c)
			}
		})
	}
}

func TestCommands(t *testing.T) {
	c := Config{Build: "go build -o {out} ./x", Start: "{out} --flag"}
	if got := c.BuildCommand("/tmp/a"); got != "go build -o '/tmp/a' ./x" {
		t.Fatal(got)
	}
	if got := c.StartCommand("/tmp/a"); got != "'/tmp/a' --flag" {
		t.Fatal(got)
	}
}

func TestCommandsQuoteOut(t *testing.T) {
	c := Config{Build: "printf %s {out}", Start: "{out}"}
	out := "/tmp/my dir/it's $(touch pwned);x"
	if got := c.BuildCommand(out); got != `printf %s '/tmp/my dir/it'\''s $(touch pwned);x'` {
		t.Fatalf("build = %s", got)
	}
	// The shell must see exactly one word equal to out.
	cmd := exec.Command("sh", "-c", `printf '%s' `+c.StartCommand(out))
	cmd.Dir = t.TempDir()
	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, got)
	}
	if string(got) != out {
		t.Fatalf("shell read %q, want %q", got, out)
	}
	if _, err := os.Stat(filepath.Join(cmd.Dir, "pwned")); err == nil {
		t.Fatal("command substitution ran")
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestLoad(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	write := func(content string) {
		if err := os.WriteFile(filepath.Join(dir, "downstream.yml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(good)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	base := git(t, dir, "rev-parse", "HEAD")

	write(strings.Replace(good, "vus: 2", "vus: 9", 1))
	git(t, dir, "commit", "-q", "-am", "head")
	head := git(t, dir, "rev-parse", "HEAD")

	b, err := Load(dir, base)
	if err != nil || b.VUs != 2 {
		t.Fatalf("base: %+v, %v", b, err)
	}
	h, err := Load(dir, head)
	if err != nil || h.VUs != 9 {
		t.Fatalf("head: %+v, %v", h, err)
	}
	if _, err := Load(dir, "0000000000000000000000000000000000000000"); err == nil {
		t.Fatal("expected error for unknown commit")
	}
	if _, err := Load(dir, "--output=x"); err == nil {
		t.Fatal("expected error for option-like commit")
	}

	write("bogus: 1\n")
	git(t, dir, "commit", "-q", "-am", "bad")
	if _, err := Load(dir, "HEAD"); err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("error = %v", err)
	}
}

func TestRepoConfigParses(t *testing.T) {
	data, err := os.ReadFile("../../downstream.yml")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if c.Root != "examples/shelf" || c.PortEnv != "PORT" || c.ReadyPath != "/health" || c.VUs != 2 || c.Iterations != 10 || c.Duration != 60*time.Second {
		t.Fatalf("unexpected config %+v", c)
	}
	if got := c.StartCommand("/x/app"); got != "'/x/app'" {
		t.Fatalf("start = %q", got)
	}
}
