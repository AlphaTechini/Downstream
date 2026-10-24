package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"downstream/engine/model"
)

type tarEntry struct {
	name, body, link string
	typ              byte
}

func makeTar(t *testing.T, entries []tarEntry) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: 0o644, Linkname: e.link}
		if typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if typ == tar.TypeDir {
			h.Mode = 0o755
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func TestExtractTarAcceptsNormalTree(t *testing.T) {
	dir := t.TempDir()
	err := extractTar(makeTar(t, []tarEntry{
		{name: "a/", typ: tar.TypeDir},
		{name: "a/b.txt", body: "hello"},
		{name: "top.txt", body: "x"},
		{name: "a/link", typ: tar.TypeSymlink, link: "b.txt"},
		{name: "a/up", typ: tar.TypeSymlink, link: "../top.txt"},
	}), dir)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a", "b.txt")); string(b) != "hello" {
		t.Errorf("file content %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a", "link")); string(b) != "hello" {
		t.Errorf("symlink inside the tree not usable: %q", b)
	}
	if _, err := os.Lstat(filepath.Join(dir, "a", "up")); err != nil {
		t.Errorf("symlink to a file inside the tree was skipped: %v", err)
	}
}

func TestExtractTarRefusesUnsafeNames(t *testing.T) {
	for _, name := range []string{"/etc/passwd", "../evil", "a/../../evil", "a/../b/../../evil", `a\..\evil`} {
		dir := t.TempDir()
		err := extractTar(makeTar(t, []tarEntry{{name: name, body: "x"}}), dir)
		if err == nil {
			t.Errorf("%q was accepted", name)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil")); err == nil {
			t.Errorf("%q wrote outside the directory", name)
		}
	}
}

func TestExtractTarSkipsEscapingLinks(t *testing.T) {
	dir := t.TempDir()
	err := extractTar(makeTar(t, []tarEntry{
		{name: "abs", typ: tar.TypeSymlink, link: "/etc/passwd"},
		{name: "rel", typ: tar.TypeSymlink, link: "../../x"},
		{name: "d/rel2", typ: tar.TypeSymlink, link: "../../x"},
		{name: "hard", typ: tar.TypeLink, link: "../x"},
		{name: "ok.txt", body: "ok"},
	}), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"abs", "rel", "d/rel2", "hard"} {
		if _, err := os.Lstat(filepath.Join(dir, n)); err == nil {
			t.Errorf("%s should have been skipped", n)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "ok.txt")); err != nil {
		t.Error("regular file missing")
	}
}

func TestExtractTarRefusesWritingThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	err := extractTar(makeTar(t, []tarEntry{
		{name: "l", typ: tar.TypeSymlink, link: "."},
		{name: "l/inner", typ: tar.TypeSymlink, link: ".."},
	}), dir)
	if err == nil {
		t.Fatal("entry below a symlink was accepted")
	}
}

func TestParseStatTicks(t *testing.T) {
	// Fields 14 and 15 are 250 and 30. The command name holds spaces and parentheses.
	stat := "1234 (my (odd) app) S 1 1234 1234 0 -1 4194560 100 0 0 0 250 30 0 0 20 0 5 0 99 1000000 500 18446744073709551615"
	got, err := parseStatTicks(stat)
	if err != nil || got != 280 {
		t.Fatalf("got %d, %v", got, err)
	}
	for _, bad := range []string{"", "1234 app S 1", "1 (a) S 1 2 3", "1 (a) S 1 1 1 0 -1 0 0 0 0 0 x 30 0 0"} {
		if _, err := parseStatTicks(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestParseVmHWM(t *testing.T) {
	status := "Name:\tapp\nVmPeak:\t  300000 kB\nVmHWM:\t   20480 kB\nVmRSS:\t   10000 kB\n"
	got, err := parseVmHWMMB(status)
	if err != nil || got != 20 {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, bad := range []string{"", "VmRSS:\t1 kB\n", "VmHWM:\tabc kB\n", "VmHWM:\t12\n"} {
		if _, err := parseVmHWMMB(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestReadSelfProc(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("no /proc")
	}
	dir, err := procDir(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readCPUTicks(dir); err != nil {
		t.Error(err)
	}
	rss, err := readPeakRSSMB(dir)
	if err != nil || rss <= 0 {
		t.Errorf("rss %v err %v", rss, err)
	}
}

func TestPickPort(t *testing.T) {
	p, err := pickPort()
	if err != nil || p < 1 || p > 65535 {
		t.Fatalf("port %d err %v", p, err)
	}
}

func TestTailBuffer(t *testing.T) {
	b := &tailBuffer{max: 10}
	io.WriteString(b, "abcdef")
	io.WriteString(b, "ghijkl")
	if b.String() != "cdefghijkl" {
		t.Errorf("got %q", b.String())
	}
	io.WriteString(b, "0123456789ABC")
	if b.String() != "3456789ABC" {
		t.Errorf("got %q", b.String())
	}
}

func TestBaselineFailure(t *testing.T) {
	j := model.Journey{ID: "j", Steps: []model.Step{{Name: "a"}, {Name: "b"}, {Name: "c"}}}
	ok := []model.StepMetrics{{Journey: "j", Step: "a", Count: 1}, {Journey: "j", Step: "b", Count: 1}, {Journey: "j", Step: "c", Count: 1}}
	if _, bad := baselineFailure(j, ok); bad {
		t.Error("healthy journey discarded")
	}
	failed := []model.StepMetrics{{Journey: "j", Step: "a", Count: 1}, {Journey: "j", Step: "b", Count: 1, Failures: 1}, {Journey: "j", Step: "c"}}
	d, bad := baselineFailure(j, failed)
	if !bad || d.Step != "b" || d.Reason != baselineReason {
		t.Errorf("got %+v %v", d, bad)
	}
	d, bad = baselineFailure(j, []model.StepMetrics{{Journey: "j", Step: "a"}})
	if !bad || d.Step != "a" {
		t.Errorf("zero-count first step: %+v %v", d, bad)
	}
	if _, bad := baselineFailure(j, []model.StepMetrics{{Journey: "other", Step: "a", Count: 5}}); !bad {
		t.Error("a journey with no metrics must be discarded")
	}
}

func TestRunWithoutK6ReportsStage(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	run, err := Run(context.Background(), Options{Repo: ".", Base: "HEAD", Head: "HEAD"})
	if err == nil || run.Error == nil || run.Error.Stage != "k6" {
		t.Fatalf("err %v run.Error %+v", err, run.Error)
	}
	if run.Passes == nil || run.Discarded == nil || run.Journeys == nil || len(run.NotMeasured) != 1 {
		t.Errorf("run is not fully populated: %+v", run)
	}
}

// ---- integration ----

const tinyGoMod = "module tinyapp\n\ngo 1.22\n"

const tinyMain = `package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

var _ = time.Millisecond

func main() {
	var n atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") })
	mux.HandleFunc("GET /fast", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "{}") })
	mux.HandleFunc("POST /items", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		fmt.Fprintf(w, "{\"id\":\"%d\"}", n.Add(1))
	})
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		// DELAY
		fmt.Fprint(w, "{}")
	})
	mux.HandleFunc("GET /gone", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	ln, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("PORT"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	http.Serve(ln, mux)
}
`

const tinyConfig = `root: .
build: go build -o {out} .
start: "{out}"
port_env: PORT
ready_path: /health
vus: 2
iterations: 15
duration: 30s
`

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func findK6(t *testing.T) string {
	if p, err := exec.LookPath("k6"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, "go", "bin", "k6")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("k6 not installed")
	return ""
}

func tinyJourneys() model.JourneyFile {
	return model.JourneyFile{Version: 1, Journeys: []model.Journey{
		{ID: "quick", Goal: "Hit the fast route.", WhyIncluded: "Unchanged route.", Steps: []model.Step{
			{Name: "fast", Method: "GET", Path: "/fast", ExpectStatus: 200},
		}},
		{ID: "items", Goal: "Create an item and read it.", WhyIncluded: "Reaches the slowed handler.", Steps: []model.Step{
			{Name: "create", Method: "POST", Path: "/items", ExpectStatus: 201, Body: json.RawMessage(`{"n":"{{uniq}}"}`), Extract: map[string]string{"id": "/id"}},
			{Name: "read", Method: "GET", Path: "/items/{{id}}", ExpectStatus: 200},
		}},
		{ID: "broken", Goal: "A journey that is already broken.", WhyIncluded: "Must be discarded.", Steps: []model.Step{
			{Name: "ok", Method: "GET", Path: "/fast", ExpectStatus: 200},
			{Name: "gone", Method: "GET", Path: "/gone", ExpectStatus: 200},
		}},
	}}
}

// tinyRepo creates a git repository with two commits; the second slows one handler.
func tinyRepo(t *testing.T) (repo, base, head string) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	repo = t.TempDir()
	git(t, repo, "init", "-q")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", tinyGoMod)
	write("main.go", tinyMain)
	write("downstream.yml", tinyConfig)
	git(t, repo, "add", ".")
	git(t, repo, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "base")
	base = git(t, repo, "rev-parse", "HEAD")
	write("main.go", strings.Replace(tinyMain, "// DELAY", "time.Sleep(30 * time.Millisecond)", 1))
	git(t, repo, "-c", "commit.gpgsign=false", "commit", "-q", "-am", "slow down the read")
	head = git(t, repo, "rev-parse", "HEAD")
	return repo, base, head
}

func tmpDirFromLog(t *testing.T, log string) string {
	t.Helper()
	m := regexp.MustCompile(`-o '(/[^']+?)/bin/`).FindStringSubmatch(log)
	if m == nil {
		t.Fatal("temp directory not found in the log")
	}
	return m[1]
}

func TestRunIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	k6 := findK6(t)
	repo, base, head := tinyRepo(t)
	statusBefore := git(t, repo, "status", "--porcelain")

	var logBuf bytes.Buffer
	run, err := Run(context.Background(), Options{Repo: repo, Base: base, Head: head, Journeys: tinyJourneys(), K6: k6, Log: &logBuf})
	if err != nil {
		t.Fatalf("Run: %v\nlog:\n%s", err, logBuf.String())
	}
	t.Logf("log:\n%s", logBuf.String())

	if run.Base != base || run.Head != head || run.Error != nil {
		t.Errorf("run header wrong: %+v", run)
	}
	if run.Settings.VUs != 2 || run.Settings.Iterations != 15 || run.Settings.DurationSeconds != 30 || !strings.HasPrefix(run.Settings.K6Version, "k6 v") {
		t.Errorf("settings %+v", run.Settings)
	}
	if len(run.Journeys) != 2 || len(run.Discarded) != 1 || run.Discarded[0].Journey != "broken" || run.Discarded[0].Step != "gone" || run.Discarded[0].Reason != baselineReason {
		t.Errorf("journeys %d discarded %+v", len(run.Journeys), run.Discarded)
	}
	if len(run.Passes) != 3 {
		t.Fatalf("passes: %d", len(run.Passes))
	}
	p95 := map[string]float64{}
	for i, want := range []struct{ label, commit string }{{"B1", base}, {"C", head}, {"B2", base}} {
		p := run.Passes[i]
		if p.Label != want.label || p.Commit != want.commit {
			t.Errorf("pass %d: %s %s", i, p.Label, p.Commit)
		}
		if _, err := time.Parse(time.RFC3339, p.StartedAt); err != nil {
			t.Errorf("started_at %q", p.StartedAt)
		}
		if p.Process.PeakRSSMB <= 0 {
			t.Errorf("%s: peak rss %v", p.Label, p.Process.PeakRSSMB)
		}
		if len(p.Steps) != 3 {
			t.Errorf("%s: %d steps, want 3 (quick/fast, items/create, items/read)", p.Label, len(p.Steps))
		}
		for _, s := range p.Steps {
			if s.Count != 30 || s.Failures != 0 {
				t.Errorf("%s %s/%s: count %d failures %d, want 30 and 0", p.Label, s.Journey, s.Step, s.Count, s.Failures)
			}
			if s.Journey == "items" && s.Step == "read" {
				p95[p.Label] = s.P95
			}
		}
	}
	if !(p95["C"] > p95["B1"] && p95["C"] > p95["B2"]) || p95["C"] < 25 {
		t.Errorf("slowed step p95: B1 %.2f C %.2f B2 %.2f", p95["B1"], p95["C"], p95["B2"])
	}
	if got := git(t, repo, "status", "--porcelain"); got != statusBefore {
		t.Errorf("repository status changed: %q", got)
	}

	// The apps must not outlive the run: no process may still run from the
	// run's temp directory, which the build lines in the log name.
	time.Sleep(200 * time.Millisecond)
	tmp := tmpDirFromLog(t, logBuf.String())
	if out, _ := exec.Command("pgrep", "-f", tmp).CombinedOutput(); strings.TrimSpace(string(out)) != "" {
		t.Errorf("processes left behind: %s", out)
	}
	if _, err := os.Stat(tmp); err == nil {
		t.Errorf("temp directory %s was not removed", tmp)
	}
}

func TestRunStopsOnBuildFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	k6 := findK6(t)
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := t.TempDir()
	git(t, repo, "init", "-q")
	for name, body := range map[string]string{"go.mod": tinyGoMod, "main.go": "package main\nfunc main() { undefined() }\n", "downstream.yml": tinyConfig} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, repo, "add", ".")
	git(t, repo, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "broken")
	run, err := Run(context.Background(), Options{Repo: repo, Base: "HEAD", Head: "HEAD", Journeys: tinyJourneys(), K6: k6})
	if err == nil || run.Error == nil || run.Error.Stage != "build (base)" || !strings.Contains(run.Error.Message, "undefined") {
		t.Fatalf("err %v, run.Error %+v", err, run.Error)
	}
}

// cancelOn cancels a context when a log line containing marker is written.
type cancelOn struct {
	marker string
	cancel context.CancelFunc
	buf    bytes.Buffer
}

func (c *cancelOn) Write(p []byte) (int, error) {
	c.buf.Write(p)
	if strings.Contains(string(p), c.marker) {
		c.cancel()
	}
	return len(p), nil
}

func TestRunCleansUpOnCancel(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	k6 := findK6(t)
	repo, base, head := tinyRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := &cancelOn{marker: "pass C", cancel: cancel}
	run, err := Run(ctx, Options{Repo: repo, Base: base, Head: head, Journeys: tinyJourneys(), K6: k6, Log: log})
	if err == nil || run.Error == nil || run.Error.Stage != "pass C" {
		t.Fatalf("err %v, run.Error %+v", err, run.Error)
	}
	if len(run.Passes) != 1 || run.Passes[0].Label != "B1" {
		t.Errorf("passes completed before the cancel should be kept: %+v", run.Passes)
	}
	time.Sleep(200 * time.Millisecond)
	tmp := tmpDirFromLog(t, log.buf.String())
	if out, _ := exec.Command("pgrep", "-f", tmp).CombinedOutput(); strings.TrimSpace(string(out)) != "" {
		t.Errorf("processes left behind: %s", out)
	}
	if _, err := os.Stat(tmp); err == nil {
		t.Errorf("temp directory %s was not removed", tmp)
	}
}

func TestWaitReadyDoesNotFollowRedirects(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
	}))
	defer other.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/health", http.StatusFound)
	}))
	defer first.Close()
	_, portStr, _ := net.SplitHostPort(first.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)

	a := &app{port: port, done: make(chan struct{}), out: &tailBuffer{max: 64}}
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	err := a.waitReady(ctx, "/health")
	if err == nil {
		t.Fatal("a redirect was treated as ready")
	}
	if n := elsewhere.Load(); n != 0 {
		t.Errorf("%d requests followed the redirect", n)
	}
}

// fakeK6 writes an executable script standing in for k6.
func fakeK6(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "k6")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

const fakeSteps = `printf '%s' '[{"journey":"j","step":"s","count":4,"failures":1,"p50_ms":2,"p95_ms":9,"mean_ms":3}]' > steps.json`

func TestRunK6KeepsStepsWhenK6ExitsNonZero(t *testing.T) {
	var log bytes.Buffer
	steps, err := runK6(context.Background(), fakeK6(t, fakeSteps+"\necho boom >&2\nexit 3"), t.TempDir(), "x", time.Second, logger{&log})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Count != 4 || steps[0].P95 != 9 {
		t.Errorf("steps = %+v", steps)
	}
	if !strings.Contains(log.String(), "k6 failed") || !strings.Contains(log.String(), "boom") {
		t.Errorf("k6 error not logged: %q", log.String())
	}
}

func TestRunK6KeepsStepsWhenK6IsKilled(t *testing.T) {
	old := k6Slack
	k6Slack = 500 * time.Millisecond
	defer func() { k6Slack = old }()
	var log bytes.Buffer
	start := time.Now()
	steps, err := runK6(context.Background(), fakeK6(t, fakeSteps+"\nsleep 60"), t.TempDir(), "x", 0, logger{&log})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 20*time.Second {
		t.Errorf("k6 was not stopped promptly")
	}
	if len(steps) != 1 || !strings.Contains(log.String(), "k6 stopped") {
		t.Errorf("steps %+v log %q", steps, log.String())
	}
}

func TestRunK6ErrorsWithoutStepsFile(t *testing.T) {
	_, err := runK6(context.Background(), fakeK6(t, "echo nope >&2\nexit 2"), t.TempDir(), "x", time.Second, logger{})
	if err == nil || !strings.Contains(err.Error(), "k6 failed") || !strings.Contains(err.Error(), "nope") {
		t.Errorf("err = %v", err)
	}
	_, err = runK6(context.Background(), fakeK6(t, "exit 0"), t.TempDir(), "x", time.Second, logger{})
	if err == nil || !strings.Contains(err.Error(), "no steps.json") {
		t.Errorf("err = %v", err)
	}
}

func TestK6Timeout(t *testing.T) {
	if k6Slack != 60*time.Second {
		t.Errorf("k6Slack = %s, want 60s", k6Slack)
	}
}
