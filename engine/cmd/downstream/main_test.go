package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"downstream/engine/model"
)

func TestCommitsRejectsNonSHA(t *testing.T) {
	for _, bad := range []string{"", "HEAD", "main", "-x", "abc", "0123456789abcdef0123456789abcdef012345678", "g123456"} {
		err := commits(t.TempDir(), bad)
		if err == nil || !strings.Contains(err.Error(), "not a commit SHA") {
			t.Errorf("commits(%q) = %v, want a SHA error", bad, err)
		}
	}
}

func TestParseRequiresFlags(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	fs.String("run", "", "")
	fs.String("scan", "", "")
	if err := parse(fs, []string{"--run", "r.json"}, "run", "scan"); err == nil || !strings.Contains(err.Error(), "--scan is required") {
		t.Errorf("missing --scan: got %v", err)
	}
	fs = flag.NewFlagSet("x", flag.ContinueOnError)
	fs.String("run", "", "")
	if err := parse(fs, []string{"--run", "r.json", "extra"}, "run"); err == nil {
		t.Error("stray argument accepted")
	}
}

func writeTemp(t *testing.T, name string, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestValidatePrintsWarningsAndStillSucceeds(t *testing.T) {
	post := model.Route{Method: "POST", Path: "/books"}
	list := model.Route{Method: "GET", Path: "/books"}
	scan := model.Scan{
		Version:  1,
		Routes:   []model.Route{post, list},
		Changed:  []model.Impact{{Route: post}},
		Affected: []model.Impact{{Route: list}},
	}
	step := func(name string, status int) model.Step {
		return model.Step{Name: name, Method: "POST", Path: "/books", ExpectStatus: status}
	}
	jf := func(steps ...model.Step) model.JourneyFile {
		return model.JourneyFile{Version: 1, Journeys: []model.Journey{{ID: "j", Goal: "g", WhyIncluded: "w", Steps: steps}}}
	}
	spath := writeTemp(t, "scan.json", scan)

	// The affected route is covered (coverage would fail otherwise); the changed route has
	// only 2xx steps, which is a warning.
	listStep := model.Step{Name: "l", Method: "GET", Path: "/books", ExpectStatus: 200}
	var out bytes.Buffer
	if err := validate(&out, writeTemp(t, "j.json", jf(step("a", 201), listStep)), spath); err != nil {
		t.Fatalf("warnings must not fail validation: %v", err)
	}
	want := "ok: 1 journeys (1 existing, 0 new, 0 control), 2 steps\n" +
		"warning: changed route POST /books is covered only by steps that expect a 2xx status; add a negative or edge-input step\n"
	if out.String() != want {
		t.Errorf("output =\n%s\nwant\n%s", out.String(), want)
	}

	out.Reset()
	jpath := writeTemp(t, "j.json", jf(step("a", 201), step("b", 401), listStep))
	if err := validate(&out, jpath, spath); err != nil {
		t.Fatal(err)
	}
	if out.String() != "ok: 1 journeys (1 existing, 0 new, 0 control), 3 steps\n" {
		t.Errorf("clean journeys printed %q", out.String())
	}

	out.Reset()
	bad := jf(model.Step{Name: "a", Method: "GET", Path: "/nowhere", ExpectStatus: 200})
	if err := validate(&out, writeTemp(t, "j.json", bad), spath); err == nil || out.Len() != 0 {
		t.Errorf("invalid journeys: err %v, output %q", err, out.String())
	}
}

func TestSummaryCountsOnlyGatedFindings(t *testing.T) {
	latency := model.Finding{ID: "j.s.p95_ms", Journey: "j", Step: "s", Metric: model.MetricP95, Unit: "ms",
		Kind: model.KindLatency, B1: 1, B2: 1, C: 400, Delta: 399, Class: model.ClassRegression}
	cpu := model.Finding{ID: "app.cpu_seconds", Metric: model.MetricCPU, Unit: "s",
		Kind: model.KindLatency, B1: 1, B2: 1, C: 40, Delta: 39, Class: model.ClassRegression}
	quiet := model.Finding{ID: "app.threads", Metric: model.MetricThreads, Unit: "count",
		Kind: model.KindCounter, Gated: true, B1: 9, B2: 10, C: 10, Class: model.ClassNoise}
	run := model.Run{Passes: []model.Pass{{Label: "B1"}, {Label: "C"}, {Label: "B2"}},
		Findings: []model.Finding{latency, cpu, quiet}}

	var out bytes.Buffer
	writeSummary(&out, run, nil)
	want := "regression=0\ntrade-off=0\nimproved=0\nwithheld=0\nadvisory=2\ndiscarded=0\npasses=3\nnew_failures=0\n"
	if out.String() != want {
		t.Errorf("latency-only run:\n%s\nwant:\n%s", out.String(), want)
	}

	run.Findings = append(run.Findings,
		model.Finding{ID: "app.cpu_ms_per_journey", Metric: model.MetricCPUPerJourney, Unit: "ms/journey",
			Kind: model.KindCPU, Gated: true, B1: 2, B2: 2, C: 240, Delta: 238, Allowed: 0.4, Class: model.ClassRegression},
		model.Finding{ID: "j.s.failures", Journey: "j", Step: "s", Metric: model.MetricFailures, Unit: "count",
			Kind: model.KindFailures, Gated: true, C: 3, Delta: 3, Class: model.ClassRegression})
	out.Reset()
	writeSummary(&out, run, nil)
	if !strings.HasPrefix(out.String(), "regression=2\n") || !strings.Contains(out.String(), "advisory=2\n") {
		t.Errorf("run with gated regressions:\n%s", out.String())
	}
}

// The run.json under evidence/runs/ci-63-engine-e2e was written before
// findings carried a Kind. Re-running summary, issues and report on it must
// give what was committed beside it: app-wide findings join the largest step
// group instead of opening an issue of their own.
func TestLegacyRunKeepsCommittedVerdict(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "evidence", "runs", "ci-63-engine-e2e", "raise-hash-cost")
	var run model.Run
	if err := readJSON(filepath.Join(dir, "run.json"), &run); err != nil {
		t.Skipf("committed run not available: %v", err)
	}
	scan, err := optionalScan(filepath.Join(dir, "scan.json"))
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	writeSummary(&out, run, scan)
	var got []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		// the committed file predates these lines
		if !strings.HasPrefix(line, "advisory=") && !strings.HasPrefix(line, "new_failures=") {
			got = append(got, line)
		}
	}
	want, err := os.ReadFile(filepath.Join(dir, "summary.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "\n") != strings.TrimSpace(string(want)) {
		t.Errorf("summary =\n%s\nwant the committed\n%s", strings.Join(got, "\n"), want)
	}

	outPath := filepath.Join(t.TempDir(), "issues.json")
	if err := cmdIssues([]string{"--run", filepath.Join(dir, "run.json"), "--scan", filepath.Join(dir, "scan.json"),
		"--mr", "1", "--starter", "u", "--out", outPath}); err != nil {
		t.Fatal(err)
	}
	titles := func(path string) []string {
		var f issuesOut
		if err := readJSON(path, &f); err != nil {
			t.Fatal(err)
		}
		var ts []string
		for _, is := range f.Issues {
			ts = append(ts, is.Title)
		}
		return ts
	}
	gotT, wantT := titles(outPath), titles(filepath.Join(dir, "issues.json"))
	if len(wantT) == 0 || strings.Join(gotT, "\n") != strings.Join(wantT, "\n") {
		t.Errorf("issue titles =\n%q\nwant the committed\n%q", gotT, wantT)
	}

	reportPath := filepath.Join(t.TempDir(), "report.md")
	if err := cmdReport([]string{"--run", filepath.Join(dir, "run.json"), "--scan", filepath.Join(dir, "scan.json"), "--out", reportPath}); err != nil {
		t.Fatal(err)
	}
	rep, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rep), "app-wide gated") {
		t.Error("report of a legacy run speaks of gated app-wide metrics")
	}
}

func TestReadableFilesListsChangedFilesAndRouteFilesOnce(t *testing.T) {
	route := func(file string) model.Impact {
		return model.Impact{Route: model.Route{Method: "GET", Path: "/x", File: file}}
	}
	s := model.Scan{
		ChangedFiles: []model.ChangedFile{{Path: "b/secret.go"}, {Path: "a/dir with space/my file.go"}},
		Changed:      []model.Impact{route("b/secret.go"), route("a/routes.go")},
		Affected:     []model.Impact{route("a/routes.go"), route("c/other.go")},
		// Routes alone are not listed: only routes in the blast radius.
		Routes: []model.Route{{File: "z/unrelated.go"}},
	}
	got := readableFiles(&s)
	want := []string{"a/dir with space/my file.go", "a/routes.go", "b/secret.go", "c/other.go"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("files = %q, want %q", got, want)
	}
	var out bytes.Buffer
	listFiles(&out, got)
	if out.String() != "1\ta/dir with space/my file.go\n2\ta/routes.go\n3\tb/secret.go\n4\tc/other.go\n" {
		t.Errorf("listing:\n%s", out.String())
	}
}

func TestReadableFilesDropsPathsThatAreNotPlainRepoPaths(t *testing.T) {
	s := model.Scan{ChangedFiles: []model.ChangedFile{
		{Path: "ok.go"}, {Path: "../escape.go"}, {Path: "/etc/passwd"}, {Path: ""}, {Path: "two\nlines.go"}, {Path: "tab\there.go"},
	}}
	if got := readableFiles(&s); len(got) != 1 || got[0] != "ok.go" {
		t.Errorf("files = %q, want only ok.go", got)
	}
}

// gitRepo makes a repository with one commit holding files, and returns its
// path and the commit SHA.
func gitRepo(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := t.TempDir()
	run := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	for name, body := range files {
		p := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", "x")
	return repo, run("rev-parse", "HEAD")
}

func TestShowPrintsTheNumberedFile(t *testing.T) {
	repo, head := gitRepo(t, map[string]string{
		"a/dir with space/my file.go": "package a // spaced\n",
		"b/secret.go":                 "package b\n",
	})
	// A command-looking name must be read as a name, not run.
	s := model.Scan{
		ChangedFiles: []model.ChangedFile{{Path: "b/secret.go"}, {Path: "a/dir with space/my file.go"}, {Path: "x; touch marker2"}},
	}
	files := readableFiles(&s)
	idx := map[string]int{}
	for i, f := range files {
		idx[f] = i + 1
	}

	var out bytes.Buffer
	if err := showFile(&out, repo, head, files, idx["a/dir with space/my file.go"]); err != nil || out.String() != "package a // spaced\n" {
		t.Errorf("spaced path: %q, %v", out.String(), err)
	}
	out.Reset()
	if err := showFile(&out, repo, head, files, idx["b/secret.go"]); err != nil || out.String() != "package b\n" {
		t.Errorf("plain path: %q, %v", out.String(), err)
	}

	// a path that shell syntax would split: git is asked for it as one name and says it is not there
	out.Reset()
	err := showFile(&out, repo, head, files, idx["x; touch marker2"])
	if err == nil || out.Len() != 0 {
		t.Errorf("missing file: %q, %v", out.String(), err)
	}
	if _, statErr := os.Stat(filepath.Join(repo, "marker2")); statErr == nil {
		t.Error("the path was run as a shell command")
	}
}

func TestShowRejectsOutOfRangeNumbers(t *testing.T) {
	repo, head := gitRepo(t, map[string]string{"a.go": "package a\n"})
	files := []string{"a.go"}
	for _, n := range []int{0, -1, 2, 1000} {
		var out bytes.Buffer
		err := showFile(&out, repo, head, files, n)
		if err == nil || !strings.Contains(err.Error(), "out of range") || out.Len() != 0 {
			t.Errorf("n=%d: %q, %v", n, out.String(), err)
		}
	}
	var out bytes.Buffer
	if err := showFile(&out, repo, head, nil, 1); err == nil {
		t.Error("an empty list accepted number 1")
	}
}

func TestShowCommandValidatesHeadAndNumber(t *testing.T) {
	repo, head := gitRepo(t, map[string]string{"a.go": "package a\n"})
	spath := writeTemp(t, "scan.json", model.Scan{ChangedFiles: []model.ChangedFile{{Path: "a.go"}}})
	for name, args := range map[string][]string{
		"head not hex": {"--scan", spath, "--repo", repo, "--head", "main", "--file", "1"},
		"head option":  {"--scan", spath, "--repo", repo, "--head", "--output=x", "--file", "1"},
		"no file":      {"--scan", spath, "--repo", repo, "--head", head},
		"zero":         {"--scan", spath, "--repo", repo, "--head", head, "--file", "0"},
		"too large":    {"--scan", spath, "--repo", repo, "--head", head, "--file", "2"},
	} {
		if err := cmdShow(args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := cmdFiles([]string{}); err == nil || !strings.Contains(err.Error(), "--scan is required") {
		t.Errorf("files without --scan: %v", err)
	}
}
