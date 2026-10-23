package runner

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"downstream/engine/model"
)

func TestMetricsSourceIsValidGo(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), metricsFileName, metricsSource, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	if f.Name.Name != "main" {
		t.Errorf("package %q", f.Name.Name)
	}
	// Only init may be declared at package level, so nothing can clash with
	// the target's own identifiers.
	if strings.Count(metricsSource, "\nfunc ") != 1 || !strings.Contains(metricsSource, "\nfunc init() {") {
		t.Errorf("the injected file declares more than init:\n%s", metricsSource)
	}
	if strings.Contains(metricsSource, "\nvar ") || strings.Contains(metricsSource, "\ntype ") || strings.Contains(metricsSource, "\nconst ") {
		t.Errorf("the injected file declares package-level names:\n%s", metricsSource)
	}
}

func TestWriteOverlay(t *testing.T) {
	dir := t.TempDir()
	tree := filepath.Join(t.TempDir(), "base-src")
	path, err := writeOverlay(dir, tree, "cmd/app")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Replace map[string]string }
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{filepath.Join(tree, "cmd", "app", metricsFileName): filepath.Join(dir, metricsFileName)}
	if !reflect.DeepEqual(got.Replace, want) {
		t.Errorf("replace map %v, want %v", got.Replace, want)
	}
	src, err := os.ReadFile(filepath.Join(dir, metricsFileName))
	if err != nil || string(src) != metricsSource {
		t.Errorf("generated file: %v", err)
	}
	// The overlay target must not exist: the tree is not touched.
	for k := range got.Replace {
		if _, err := os.Stat(k); err == nil {
			t.Errorf("%s exists on disk", k)
		}
	}
	if _, err := writeOverlay(dir, "relative/tree", "x"); err == nil {
		t.Error("a relative tree path was accepted")
	}
	// "." as main_dir puts the file at the root of the tree.
	path, err = writeOverlay(t.TempDir(), tree, ".")
	if err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if !strings.Contains(string(b), filepath.Join(tree, metricsFileName)) {
		t.Errorf("root overlay: %s", b)
	}
}

func TestWithOverlayFlag(t *testing.T) {
	got, err := withOverlayFlag([]string{"A=1", "GOFLAGS=-mod=mod", "B=2"}, "/tmp/o/overlay.json")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"A=1", "GOFLAGS=-mod=mod -overlay=/tmp/o/overlay.json", "B=2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
	got, _ = withOverlayFlag([]string{"A=1"}, "/tmp/o/overlay.json")
	if want := []string{"A=1", "GOFLAGS=-overlay=/tmp/o/overlay.json"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
	got, _ = withOverlayFlag([]string{"GOFLAGS="}, "/o.json")
	if want := []string{"GOFLAGS=-overlay=/o.json"}; !reflect.DeepEqual(got, want) {
		t.Errorf("empty GOFLAGS: got %q", got)
	}
	if _, err := withOverlayFlag(nil, "/tmp/a b/o.json"); err == nil {
		t.Error("a path with a space was accepted")
	}
}

func TestParseRuntimeCounters(t *testing.T) {
	rc, err := parseRuntimeCounters([]byte(`{"alloc_objects":10,"alloc_bytes":2048,"gc_cycles":3,"goroutines":7,"extra":1}`))
	if err != nil || rc != (runtimeCounters{AllocObjects: 10, AllocBytes: 2048, GCCycles: 3, Goroutines: 7}) {
		t.Errorf("%+v %v", rc, err)
	}
	// Above 2^53, where a float64 would lose the low bits.
	rc, err = parseRuntimeCounters([]byte(`{"alloc_objects":18446744073709551615,"alloc_bytes":9007199254740993,"gc_cycles":0,"goroutines":1}`))
	if err != nil || rc.AllocObjects != 18446744073709551615 || rc.AllocBytes != 9007199254740993 {
		t.Errorf("large values: %+v %v", rc, err)
	}
	for name, body := range map[string]string{
		"missing key": `{"alloc_objects":1,"alloc_bytes":2,"gc_cycles":3}`,
		"null":        `{"alloc_objects":1,"alloc_bytes":2,"gc_cycles":3,"goroutines":null}`,
		"negative":    `{"alloc_objects":-1,"alloc_bytes":2,"gc_cycles":3,"goroutines":4}`,
		"string":      `{"alloc_objects":"1","alloc_bytes":2,"gc_cycles":3,"goroutines":4}`,
		"fraction":    `{"alloc_objects":1.5,"alloc_bytes":2,"gc_cycles":3,"goroutines":4}`,
		"not json":    `<html>`,
		"empty":       ``,
		"array":       `[1,2,3,4]`,
	} {
		if _, err := parseRuntimeCounters([]byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCounterDelta(t *testing.T) {
	if d, err := counterDelta("x", 5, 9); d != 4 || err != nil {
		t.Errorf("%d %v", d, err)
	}
	if _, err := counterDelta("x", 9, 5); err == nil {
		t.Error("a backwards counter was accepted")
	}
}

func dirSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if info.IsDir() {
			out[rel] = "dir"
			return nil
		}
		b, err := os.ReadFile(p)
		out[rel] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestOverlayBuildServesMetrics builds a tiny target with the injected file and
// checks that it serves /metrics only when asked to and that the tree it was
// built from is unchanged.
func TestOverlayBuildServesMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not on PATH")
	}
	tree := t.TempDir()
	app := filepath.Join(tree, "cmd", "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"go.mod":       tinyGoMod,
		"cmd/app/a.go": "package main\n\nimport (\n\t\"net/http\"\n\t\"os\"\n)\n\nfunc main() {\n\thttp.HandleFunc(\"/health\", func(w http.ResponseWriter, r *http.Request) {})\n\thttp.ListenAndServe(\"127.0.0.1:\"+os.Getenv(\"PORT\"), nil)\n}\n",
	} {
		if err := os.WriteFile(filepath.Join(tree, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before := dirSnapshot(t, tree)

	scratch := t.TempDir()
	overlay, err := writeOverlay(filepath.Join(scratch), tree, "cmd/app")
	if err != nil {
		t.Fatal(err)
	}
	env, err := withOverlayFlag(os.Environ(), overlay)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(scratch, "app")
	cmd := exec.Command(goBin, "build", "-o", bin, "./cmd/app")
	cmd.Dir, cmd.Env = tree, env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build with the overlay: %v\n%s", err, out)
	}
	if after := dirSnapshot(t, tree); !reflect.DeepEqual(before, after) {
		t.Errorf("the tree changed:\nbefore %v\nafter  %v", before, after)
	}

	start := func(extra ...string) (stop func()) {
		c := exec.Command(bin)
		c.Env = append(os.Environ(), extra...)
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		return func() { _ = c.Process.Kill(); _ = c.Wait() }
	}
	port, err := pickPort()
	if err != nil {
		t.Fatal(err)
	}
	mport, err := pickPort()
	if err != nil {
		t.Fatal(err)
	}
	addr := "127.0.0.1:" + strconv.Itoa(mport)
	stop := start("PORT="+strconv.Itoa(port), metricsAddrEnv+"="+addr)
	defer stop()
	rc, err := firstRuntimeCounters(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	if rc.AllocObjects == 0 || rc.AllocBytes == 0 || rc.Goroutines < 1 {
		t.Errorf("counters %+v", rc)
	}
	rc2, err := readRuntimeCounters(context.Background(), addr)
	if err != nil || rc2.AllocObjects < rc.AllocObjects || rc2.AllocBytes < rc.AllocBytes || rc2.GCCycles < rc.GCCycles {
		t.Errorf("second reading %+v after %+v, err %v", rc2, rc, err)
	}
	stop()

	// Without the environment variable the same binary opens no side port.
	port2, _ := pickPort()
	mport2, _ := pickPort()
	stop2 := start("PORT=" + strconv.Itoa(port2))
	defer stop2()
	time.Sleep(300 * time.Millisecond)
	if _, err := readRuntimeCounters(context.Background(), "127.0.0.1:"+strconv.Itoa(mport2)); err == nil {
		t.Error("the side port answered although the variable was not set")
	}
}

const quickConfig = `root: .
%s
build: go build -o {out} .
start: "{out}"
port_env: PORT
ready_path: /health
vus: 1
iterations: 3
duration: 30s
`

func quickRun(t *testing.T, mainDirLine string, extra map[string]string, mainSrc string) model.Run {
	t.Helper()
	k6 := findK6(t)
	if mainSrc != "" {
		if extra == nil {
			extra = map[string]string{}
		}
		extra["main.go"] = mainSrc
	}
	repo, base, _ := tinyRepoWith(t, strings.Replace(quickConfig, "%s\n", mainDirLine, 1), extra)
	var logBuf strings.Builder
	run, err := Run(context.Background(), Options{Repo: repo, Base: base, Head: base, Journeys: tinyJourneys(), K6: k6, Log: &logBuf})
	if err != nil {
		t.Fatalf("Run: %v\nlog:\n%s", err, logBuf.String())
	}
	if len(run.Passes) != 3 {
		t.Fatalf("passes: %d", len(run.Passes))
	}
	return run
}

func wantNoRuntimeCounters(t *testing.T, run model.Run) []string {
	t.Helper()
	four := []string{"alloc_objects_per_journey", "alloc_bytes_per_journey", "gc_cycles_per_journey", "goroutines"}
	for _, p := range run.Passes {
		var got []string
		for _, u := range p.Process.Unmeasured {
			if strings.HasPrefix(u, "alloc_") || strings.HasPrefix(u, "gc_") || u == "goroutines" {
				got = append(got, u)
			}
		}
		if !reflect.DeepEqual(got, four) {
			t.Errorf("%s: unmeasured %v", p.Label, p.Process.Unmeasured)
		}
		ps := p.Process
		if ps.AllocObjectsPerJourney != 0 || ps.AllocBytesPerJourney != 0 || ps.GCCyclesPerJourney != 0 || ps.Goroutines != 0 {
			t.Errorf("%s: unmeasured counters carry values: %+v", p.Label, ps)
		}
		if ps.CompletedJourneys == 0 || ps.CPUMsPerJourney <= 0 {
			t.Errorf("%s: the other counters were lost: %+v", p.Label, ps)
		}
	}
	var lines []string
	for _, n := range run.NotMeasured {
		if strings.HasPrefix(n, "runtime counters: not measured") {
			lines = append(lines, n)
		}
	}
	return lines
}

func TestRunWithoutMainDirDoesNotMeasureRuntimeCounters(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	run := quickRun(t, "", nil, "")
	lines := wantNoRuntimeCounters(t, run)
	if len(lines) != 1 || !strings.Contains(lines[0], "no main_dir") {
		t.Errorf("not_measured %q", run.NotMeasured)
	}
}

// libMain imports a package that is not main, so an injected file declaring
// package main in that package's directory breaks the build.
const libMain = "package main\n\nimport _ \"tinyapp/lib\"\n\n"

func TestRunFallsBackWhenMainDirPackageIsNotMain(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	src := strings.Replace(tinyMain, "package main\n\n", libMain, 1)
	run := quickRun(t, "main_dir: lib\n", map[string]string{"lib/lib.go": "package lib\n"}, src)
	lines := wantNoRuntimeCounters(t, run)
	if len(lines) != 1 || !strings.Contains(lines[0], "build with the injected file failed, so it was built without it") {
		t.Errorf("not_measured %q", run.NotMeasured)
	}
}

func TestRunNotesMainDirOutsideTheBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	// The directory exists and builds with the injected file, but the build
	// command never compiles it, so the binary serves no /metrics.
	run := quickRun(t, "main_dir: unused\n", map[string]string{"unused/x.go": "package main\n\nfunc main() {}\n"}, "")
	lines := wantNoRuntimeCounters(t, run)
	if len(lines) != 1 || !strings.Contains(lines[0], "the injected /metrics endpoint did not answer") {
		t.Errorf("not_measured %q", run.NotMeasured)
	}
}

func TestRunNotesMissingMainDir(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	run := quickRun(t, "main_dir: nowhere\n", nil, "")
	lines := wantNoRuntimeCounters(t, run)
	if len(lines) != 1 || !strings.Contains(lines[0], "main_dir nowhere is not a directory") {
		t.Errorf("not_measured %q", run.NotMeasured)
	}
}
