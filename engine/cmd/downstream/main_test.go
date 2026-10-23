package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
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

	var out bytes.Buffer
	if err := validate(&out, writeTemp(t, "j.json", jf(step("a", 201))), spath); err != nil {
		t.Fatalf("warnings must not fail validation: %v", err)
	}
	want := "ok: 1 journeys, 1 steps\n" +
		"warning: changed route POST /books is covered only by steps that expect a 2xx status; add a negative or edge-input step\n" +
		"warning: affected route GET /books is not covered by any journey\n"
	if out.String() != want {
		t.Errorf("output =\n%s\nwant\n%s", out.String(), want)
	}

	out.Reset()
	jpath := writeTemp(t, "j.json", jf(step("a", 201), step("b", 401)))
	scan.Affected = nil
	spath = writeTemp(t, "scan.json", scan)
	if err := validate(&out, jpath, spath); err != nil {
		t.Fatal(err)
	}
	if out.String() != "ok: 1 journeys, 2 steps\n" {
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
	want := "regression=0\ntrade-off=0\nimproved=0\nwithheld=0\nadvisory=2\ndiscarded=0\npasses=3\n"
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
