package report

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"downstream/engine/model"
)

var update = flag.Bool("update", false, "rewrite golden files")

func load[T any](t *testing.T, name string) T {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("output differs from %s (run with -update after checking the change)\n--- got ---\n%s", path, got)
	}
}

func TestGoldenFull(t *testing.T) {
	run := load[model.Run](t, "run_basic.json")
	scan := load[model.Scan](t, "scan_basic.json")
	golden(t, "report_basic.golden.md", Render(run, &scan))
}

func TestGoldenNoScan(t *testing.T) {
	run := load[model.Run](t, "run_basic.json")
	out := Render(run, nil)
	if strings.Contains(out, "Blast radius") {
		t.Errorf("blast radius rendered without a scan")
	}
}

func TestGoldenError(t *testing.T) {
	run := load[model.Run](t, "run_error.json")
	out := Render(run, nil)
	golden(t, "report_error.golden.md", out)
	for _, want := range []string{"**Run stopped at build (head)**", "No comparison was made.", "undefined: shelfTable"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "mermaid") || strings.Contains(out, "**Findings:**") {
		t.Errorf("rendered a comparison for a run with no passes")
	}
}

// Every figure in the fixture must appear in the output formatted with the
// documented decimals, so a number cannot be dropped or reformatted silently.
func TestFiguresAppear(t *testing.T) {
	run := load[model.Run](t, "run_basic.json")
	out := Render(run, nil)
	for _, f := range run.Findings {
		if f.Metric != model.MetricP95 && f.Class == model.ClassNoise {
			continue
		}
		if f.Journey == "" && f.Class == model.ClassNoise {
			continue
		}
		// p50 and mean rows are left out when they repeat the p95 verdict.
		if f.Metric == model.MetricP50 || f.Metric == model.MetricMean {
			if p, ok := findingByID(run, f.Journey+"."+f.Step+"."+model.MetricP95); ok && p.Class == f.Class {
				continue
			}
		}
		for _, v := range []float64{f.B1, f.C, f.B2, f.Band} {
			s := val(v, f.Unit)
			if !strings.Contains(out, s) {
				t.Errorf("%s: %q not in output", f.ID, s)
			}
		}
		if s := signed(f.Delta, decimals(f.Unit)); !strings.Contains(out, s) {
			t.Errorf("%s: delta %q not in output", f.ID, s)
		}
	}
	for _, p := range run.Passes {
		for _, s := range []string{val(p.Process.CPUSeconds, "s"), val(p.Process.PeakRSSMB, "MB")} {
			if !strings.Contains(out, s) {
				t.Errorf("pass %s: %q not in output", p.Label, s)
			}
		}
	}
}

func decimals(unit string) int {
	switch unit {
	case "s":
		return 2
	case "count":
		return 0
	}
	return 1
}

// Numbers in the output must come from the input or be fixed text: collect
// every number token in the output and require it in an allowed set built from
// the fixture.
func TestNoUnexplainedNumbers(t *testing.T) {
	run := load[model.Run](t, "run_basic.json")
	out := Render(run, nil)
	allowed := map[string]bool{"1": true, "2": true, "3": true, "4": true, "5": true, "95": true, "20": true, "50": true, "10": true}
	for _, f := range run.Findings {
		for _, u := range []string{f.Unit} {
			for _, v := range []float64{f.B1, f.C, f.B2, f.Band, f.Delta} {
				allowed[strings.TrimPrefix(val(v, u), "-")] = true
			}
		}
		allowed[strings.TrimPrefix(strconv.FormatFloat(f.DeltaPct, 'f', 0, 64), "-")] = true
	}
	for _, p := range run.Passes {
		allowed[val(p.Process.CPUSeconds, "s")] = true
		allowed[val(p.Process.PeakRSSMB, "MB")] = true
	}
	allowed[strconv.Itoa(run.Settings.VUs)] = true
	allowed[strconv.Itoa(run.Settings.DurationSeconds)] = true
	for _, s := range []string{run.Base, run.Head, run.StartedAt, run.EndedAt, run.Settings.K6Version} {
		for _, n := range numRe.FindAllString(s, -1) {
			allowed[n] = true
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Thresholds in effect") || strings.HasPrefix(line, "Candidate `") ||
			strings.HasPrefix(line, "**Findings:**") || strings.HasPrefix(line, "- ") && !strings.Contains(line, "|") {
			continue
		}
		for _, n := range numRe.FindAllString(line, -1) {
			if !allowed[n] {
				t.Errorf("number %q in line %q is not derived from the input", n, line)
			}
		}
	}
}

var numRe = regexp.MustCompile(`\d+(?:\.\d+)?`)

func TestChartLimit(t *testing.T) {
	run := model.Run{
		Settings: model.Settings{VUs: 1, DurationSeconds: 5, K6Version: "v0.56.0"},
		Passes:   []model.Pass{{Label: "B1"}, {Label: "C"}, {Label: "B2"}},
	}
	j := model.Journey{ID: "j", Goal: `Goal with "quotes"`}
	for i := 0; i < 25; i++ {
		name := "s" + strconv.Itoa(i)
		j.Steps = append(j.Steps, model.Step{Name: name})
		run.Findings = append(run.Findings, model.Finding{
			ID: "j." + name + ".p95_ms", Journey: "j", Step: name, Metric: model.MetricP95, Unit: "ms",
			B1: 100, B2: 100, C: 100 + float64(i), Delta: float64(i), DeltaPct: float64(i), Class: model.ClassNoise,
		})
	}
	run.Journeys = []model.Journey{j}
	out := Render(run, nil)
	if got := strings.Count(out, `"j1.s`); got != 20 {
		t.Errorf("charted %d steps, want 20", got)
	}
	if strings.Contains(out, `"j1.s0"`) || !strings.Contains(out, `"j1.s24"`) {
		t.Errorf("chart did not keep the steps with the largest change")
	}
	if !strings.Contains(out, "The chart shows the 20 steps with the largest change, out of 25.") {
		t.Errorf("truncation not stated")
	}
}

func TestLabelEscaping(t *testing.T) {
	if got := chartLabel(`j1.a"b'c`); strings.ContainsAny(got, `"'`) {
		t.Errorf("label %q still has quotes", got)
	}
}

func TestSigned(t *testing.T) {
	for _, tc := range []struct {
		v    float64
		dec  int
		want string
	}{{12.34, 1, "+12.3"}, {-0.04, 1, "+0.0"}, {-2.5, 1, "-2.5"}, {0, 0, "+0"}, {-0.3, 0, "+0"}} {
		if got := signed(tc.v, tc.dec); got != tc.want {
			t.Errorf("signed(%v,%d) = %q, want %q", tc.v, tc.dec, got, tc.want)
		}
	}
}

func TestChangeWithoutPercentWhenBaselineZero(t *testing.T) {
	f := model.Finding{Unit: "count", B1: 0, B2: 0, C: 30, Delta: 30}
	if got := change(f); got != "+30" {
		t.Errorf("change = %q", got)
	}
}

func TestSummaryAndIssuesToOpen(t *testing.T) {
	run := load[model.Run](t, "run_basic.json")
	scan := load[model.Scan](t, "scan_basic.json")
	out := Render(run, &scan)
	for _, w := range []string{
		"**Findings:** regressed steps 2, trade-off journeys 1, improved metrics 0, withheld metrics 3. App process regressions: 2.",
		"**Issues to open**\n\n- `shelf.add_book.p95_ms`\n- `signin.login.failures`\n\n2 regressed steps are grouped into 2 by the changed functions",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q", w)
		}
	}
	// without a scan both steps share one group, headed by the failures finding
	if out := Render(run, nil); !strings.Contains(out, "**Issues to open**\n\n- `signin.login.failures` (2 steps)\n\n2 regressed steps are grouped into 1 by") {
		t.Errorf("no-scan grouping wrong:\n%s", out)
	}

	// no regressions: no app clause, issues "none"
	var keep []model.Finding
	for _, f := range run.Findings {
		if f.Class != model.ClassRegression {
			keep = append(keep, f)
		}
	}
	run.Findings = keep
	out = Render(run, &scan)
	if !strings.Contains(out, "regressed steps 0, trade-off journeys 1, improved metrics 0, withheld metrics 3.\n") {
		t.Errorf("summary without regressions wrong:\n%s", out)
	}
	if strings.Contains(out, "App process regressions") {
		t.Errorf("app clause shown without app regressions")
	}
	if !strings.Contains(out, "**Issues to open**\n\nnone\n") {
		t.Errorf("issues to open should say none")
	}

	// app-only regression: one combined headline, peak RSS first
	run = load[model.Run](t, "run_basic.json")
	keep = nil
	for _, f := range run.Findings {
		if f.Journey != "" && f.Class == model.ClassRegression {
			continue
		}
		keep = append(keep, f)
	}
	run.Findings = keep
	out = Render(run, nil)
	if !strings.Contains(out, "regressed steps 0,") || !strings.Contains(out, "App process regressions: 2.") ||
		!strings.Contains(out, "**Issues to open**\n\n- `app.peak_rss_mb`\n") {
		t.Errorf("app-only summary wrong:\n%s", out)
	}
}

func findingByID(run model.Run, id string) (model.Finding, bool) {
	for _, f := range run.Findings {
		if f.ID == id {
			return f, true
		}
	}
	return model.Finding{}, false
}

func TestIssuesToOpenCap(t *testing.T) {
	run := model.Run{
		Settings: model.Settings{VUs: 1, DurationSeconds: 5},
		Passes:   []model.Pass{{Label: "B1"}, {Label: "C"}, {Label: "B2"}},
	}
	scan := &model.Scan{}
	for g := 1; g <= 7; g++ {
		jid := "j" + strconv.Itoa(g)
		path := "/r" + strconv.Itoa(g)
		scan.Changed = append(scan.Changed, model.Impact{Route: model.Route{Method: "GET", Path: path}, ChangedFuncs: []string{"f" + strconv.Itoa(g)}})
		j := model.Journey{ID: jid, Goal: jid}
		for s := 1; s <= g; s++ {
			name := "s" + strconv.Itoa(s)
			j.Steps = append(j.Steps, model.Step{Name: name, Method: "GET", Path: path})
			run.Findings = append(run.Findings, model.Finding{ID: jid + "." + name + ".p95_ms", Journey: jid, Step: name,
				Metric: model.MetricP95, Unit: "ms", Delta: 1, Class: model.ClassRegression})
		}
		run.Journeys = append(run.Journeys, j)
	}
	out := Render(run, scan)
	for _, w := range []string{
		"28 regressed steps are grouped into 7 by",
		"The 5 largest groups are listed. 2 other groups (3 steps) are left out",
		"- `j7.s1.p95_ms` (7 steps)",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
}
