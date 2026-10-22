package classify

import (
	"sort"
	"strings"
	"testing"

	"downstream/engine/model"
)

type vals struct{ b1, c, b2 float64 }

// passes builds three passes with one step j.s, taking p95 from p95 and the
// failure count from fail. Other figures are equal everywhere.
func passes(p95 vals, fail vals) []model.Pass {
	mk := func(label string, p float64, f float64) model.Pass {
		return model.Pass{
			Label:   label,
			Process: model.ProcessStats{CPUSeconds: 1, PeakRSSMB: 50},
			Steps: []model.StepMetrics{{
				Journey: "j", Step: "s", Count: 100, Failures: int(f),
				P50: 20, P95: p, Mean: 25,
			}},
		}
	}
	return []model.Pass{mk("B1", p95.b1, fail.b1), mk("C", p95.c, fail.c), mk("B2", p95.b2, fail.b2)}
}

func find(t *testing.T, fs []model.Finding, id string) model.Finding {
	t.Helper()
	for _, f := range fs {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("finding %q not found in %d findings", id, len(fs))
	return model.Finding{}
}

func TestClassifyP95(t *testing.T) {
	cases := []struct {
		name  string
		p95   vals
		class string
		delta float64
		pct   float64
		band  float64
		word  string // must appear in the reason
	}{
		{"noise", vals{100, 102, 104}, model.ClassNoise, -2, -100.0 * 2 / 104, 4, "inside the range"},
		{"regression", vals{100, 300, 104}, model.ClassRegression, 196, 100.0 * 196 / 104, 4, "more than 2 times the noise band of 4.0 ms"},
		{"below floor", vals{2, 8, 2.2}, model.ClassNoise, 5.8, 100.0 * 5.8 / 2.2, 0.2, "below the minimum of 10.0 ms"},
		{"below relative", vals{1000, 1105, 1010}, model.ClassNoise, 95, 100.0 * 95 / 1010, 10, "below the minimum of 20%"},
		{"inside band", vals{100, 130, 150}, model.ClassNoise, -20, -100.0 * 20 / 150, 50, "inside the range"},
		{"just over band, small", vals{100, 160, 140}, model.ClassNoise, 20, 100.0 * 20 / 140, 40, "not more than 2 times the noise band of 40.0 ms"},
		{"withheld", vals{100, 400, 180}, model.ClassWithheld, 220, 100.0 * 220 / 180, 80, "baseline spread rule"},
		{"spread below floor is not withheld", vals{2.6, 536.6, 4.6}, model.ClassRegression, 532, 100.0 * 532 / 4.6, 2, "more than 2 times the noise band of 2.0 ms"},
		{"improved", vals{200, 80, 204}, model.ClassImproved, 80 - 204, 100.0 * (80 - 204) / 204, 4, "better than the better baseline by 120.0 ms"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := Classify(passes(tc.p95, vals{}), DefaultThresholds())
			f := find(t, fs, "j.s.p95_ms")
			if f.Class != tc.class {
				t.Errorf("class = %q (%s), want %q", f.Class, f.Reason, tc.class)
			}
			if !strings.Contains(f.Reason, tc.word) {
				t.Errorf("reason %q does not contain %q", f.Reason, tc.word)
			}
			if !near(f.Delta, tc.delta) || !near(f.DeltaPct, tc.pct) || !near(f.Band, tc.band) {
				t.Errorf("delta/pct/band = %v/%v/%v, want %v/%v/%v", f.Delta, f.DeltaPct, f.Band, tc.delta, tc.pct, tc.band)
			}
			if f.Unit != "ms" || f.Journey != "j" || f.Step != "s" || f.Metric != model.MetricP95 {
				t.Errorf("identity fields wrong: %+v", f)
			}
		})
	}
}

func near(a, b float64) bool {
	d := a - b
	return d < 1e-9 && d > -1e-9
}

func TestFailures(t *testing.T) {
	cases := []struct {
		name  string
		fail  vals
		class string
		want  string
	}{
		{"regression", vals{0, 30, 0}, model.ClassRegression, "passed on both baseline runs, failed 30 times on the candidate"},
		{"single", vals{0, 1, 0}, model.ClassRegression, "failed 1 time on the candidate"},
		{"baseline failed too", vals{0, 30, 2}, model.ClassNoise, "failed on the baseline too"},
		{"none", vals{0, 0, 0}, model.ClassNoise, "no failures"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := Classify(passes(vals{100, 100, 100}, tc.fail), DefaultThresholds())
			f := find(t, fs, "j.s.failures")
			if f.Class != tc.class || !strings.Contains(f.Reason, tc.want) {
				t.Errorf("got %q / %q, want %q containing %q", f.Class, f.Reason, tc.class, tc.want)
			}
			if f.Unit != "count" {
				t.Errorf("unit = %q", f.Unit)
			}
		})
	}
}

func TestFailureDelta(t *testing.T) {
	f := find(t, Classify(passes(vals{100, 100, 100}, vals{0, 30, 0}), DefaultThresholds()), "j.s.failures")
	if f.Delta != 30 || f.DeltaPct != 0 {
		t.Errorf("delta %v pct %v, want 30 and 0 (worse baseline is 0)", f.Delta, f.DeltaPct)
	}
}

func TestAppMetrics(t *testing.T) {
	ps := passes(vals{100, 100, 100}, vals{})
	ps[0].Process = model.ProcessStats{CPUSeconds: 1.00, PeakRSSMB: 50}
	ps[1].Process = model.ProcessStats{CPUSeconds: 2.00, PeakRSSMB: 80}
	ps[2].Process = model.ProcessStats{CPUSeconds: 1.02, PeakRSSMB: 51}
	fs := Classify(ps, DefaultThresholds())
	cpu := find(t, fs, "app.cpu_seconds")
	if cpu.Class != model.ClassRegression || cpu.Unit != "s" || cpu.Journey != "" || cpu.Step != "" {
		t.Errorf("cpu: %+v", cpu)
	}
	rss := find(t, fs, "app.peak_rss_mb")
	if rss.Class != model.ClassRegression || rss.Unit != "MB" {
		t.Errorf("rss: %+v", rss)
	}
}

func TestTradeOff(t *testing.T) {
	mk := func(label string, p95, p50 float64) model.Pass {
		return model.Pass{Label: label, Steps: []model.StepMetrics{
			{Journey: "j", Step: "a", P95: p95, P50: 20, Mean: 25},
			{Journey: "j", Step: "b", P95: 100, P50: p50, Mean: 25},
			{Journey: "k", Step: "a", P95: 100, P50: 20, Mean: 25},
		}}
	}
	// step a p95 regresses, step b p50 improves, same journey j.
	ps := []model.Pass{mk("B1", 100, 200), mk("C", 300, 80), mk("B2", 102, 204)}
	fs := Classify(ps, DefaultThresholds())
	for _, id := range []string{"j.a.p95_ms", "j.b.p50_ms"} {
		f := find(t, fs, id)
		if f.Class != model.ClassTradeOff || f.Reason != "one metric clearly better and another clearly worse on the same journey" {
			t.Errorf("%s: %q %q", id, f.Class, f.Reason)
		}
	}
	if f := find(t, fs, "k.a.p95_ms"); f.Class != model.ClassNoise {
		t.Errorf("other journey changed class: %+v", f)
	}
}

func TestTradeOffIgnoresFailures(t *testing.T) {
	mk := func(label string, p95 float64, fail int) model.Pass {
		return model.Pass{Label: label, Steps: []model.StepMetrics{
			{Journey: "j", Step: "a", P95: p95, P50: 20, Mean: 25, Failures: fail},
		}}
	}
	// p95 improves, failures regress: no latency regression, so no trade-off.
	fs := Classify([]model.Pass{mk("B1", 200, 0), mk("C", 80, 5), mk("B2", 204, 0)}, DefaultThresholds())
	if f := find(t, fs, "j.a.failures"); f.Class != model.ClassRegression {
		t.Errorf("failures class = %q", f.Class)
	}
	if f := find(t, fs, "j.a.p95_ms"); f.Class != model.ClassImproved {
		t.Errorf("p95 class = %q", f.Class)
	}
}

func TestMissingPass(t *testing.T) {
	ps := passes(vals{100, 100, 100}, vals{})
	if got := Classify(ps[:2], DefaultThresholds()); got != nil {
		t.Errorf("two passes: got %d findings, want nil", len(got))
	}
	ps[2].Label = "B3"
	if got := Classify(ps, DefaultThresholds()); got != nil {
		t.Errorf("unknown label: got %d findings, want nil", len(got))
	}
	if got := Classify(nil, DefaultThresholds()); got != nil {
		t.Errorf("no passes: want nil")
	}
}

func TestStepMustBeInAllPasses(t *testing.T) {
	ps := passes(vals{100, 100, 100}, vals{})
	ps[1].Steps = append(ps[1].Steps, model.StepMetrics{Journey: "j", Step: "only_c", P95: 5})
	fs := Classify(ps, DefaultThresholds())
	for _, f := range fs {
		if f.Step == "only_c" {
			t.Errorf("step present in one pass only was judged: %s", f.ID)
		}
	}
}

func TestOrderAndCount(t *testing.T) {
	ps := passes(vals{100, 100, 100}, vals{})
	fs := Classify(ps, DefaultThresholds())
	if len(fs) != 6 {
		t.Fatalf("got %d findings, want 4 step metrics + 2 app metrics", len(fs))
	}
	if !sort.SliceIsSorted(fs, func(i, j int) bool { return fs[i].ID < fs[j].ID }) {
		t.Errorf("findings not sorted by ID")
	}
	again := Classify(ps, DefaultThresholds())
	for i := range fs {
		if fs[i] != again[i] {
			t.Fatalf("not deterministic at %d", i)
		}
	}
}

func TestDefaults(t *testing.T) {
	d := DefaultThresholds()
	if d.BandMultiplier != 2 || d.MinRelative != 0.20 || d.MinLatencyMS != 10 || d.MinMemoryMB != 10 || d.MaxBaselineSpread != 0.5 {
		t.Errorf("defaults: %+v", d)
	}
}
