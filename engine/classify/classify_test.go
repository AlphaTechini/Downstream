package classify

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"sort"
	"strconv"
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
		{"withheld", vals{100, 150, 180}, model.ClassWithheld, -30, -100.0 * 30 / 180, 80, "baseline spread rule"},
		{"big regression beats spread rule", vals{10, 1000, 30}, model.ClassRegression, 970, 100.0 * 970 / 30, 20, "more than 2 times the noise band of 20.0 ms"},
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
			if f.Kind != model.KindLatency || f.Gated {
				t.Errorf("latency must be advisory: kind %q gated %v", f.Kind, f.Gated)
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
	if cpu.Kind != model.KindLatency || cpu.Gated {
		t.Errorf("total CPU seconds must be advisory: %+v", cpu)
	}
	rss := find(t, fs, "app.peak_rss_mb")
	if rss.Class != model.ClassRegression || rss.Unit != "MB" || rss.Kind != model.KindMemory || !rss.Gated {
		t.Errorf("rss: %+v", rss)
	}
}

func TestLatencyNeverTradesOff(t *testing.T) {
	mk := func(label string, p95, p50 float64) model.Pass {
		return model.Pass{Label: label, Steps: []model.StepMetrics{
			{Journey: "j", Step: "a", Count: 10, P95: p95, P50: 20, Mean: 25},
			{Journey: "j", Step: "b", Count: 10, P95: 100, P50: p50, Mean: 25},
		}}
	}
	// step a p95 regresses, step b p50 improves, same journey: both keep their class.
	fs := Classify([]model.Pass{mk("B1", 100, 200), mk("C", 300, 80), mk("B2", 102, 204)}, DefaultThresholds())
	if f := find(t, fs, "j.a.p95_ms"); f.Class != model.ClassRegression || f.Gated {
		t.Errorf("a: %+v", f)
	}
	if f := find(t, fs, "j.b.p50_ms"); f.Class != model.ClassImproved || f.Gated {
		t.Errorf("b: %+v", f)
	}
}

func TestTradeOffIgnoresFailures(t *testing.T) {
	mk := func(label string, p95 float64, fail int) model.Pass {
		return model.Pass{Label: label, Steps: []model.StepMetrics{
			{Journey: "j", Step: "a", Count: 10, P95: p95, P50: 20, Mean: 25, Failures: fail},
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
	if d.BandMultiplier != 2 || d.MinRelative != 0.20 || d.MinLatencyMS != 10 || d.MinMemoryMB != 10 || d.MinCPUSeconds != 0.5 || d.MaxBaselineSpread != 0.5 {
		t.Errorf("defaults: %+v", d)
	}
	want := map[string]model.Tolerance{
		"write_bytes_per_journey":    {Relative: 0.01, Absolute: 1 << 10},
		"write_syscalls_per_journey": {Relative: 0.04},
		"open_fds":                   {Absolute: 1},
		"threads":                    {Absolute: 2},
		"cpu_ms_per_journey":         {Relative: 0.20},
		"peak_rss_mb":                {Relative: 0.05, Absolute: 10},

		"alloc_objects_per_journey": {Relative: 0.10, Absolute: 100},
		"alloc_bytes_per_journey":   {Relative: 0.05, Absolute: 16 << 10},
		"gc_cycles":                 {Absolute: 2},
		"goroutines":                {Absolute: 1},
	}
	if len(d.Tolerances) != len(want) {
		t.Errorf("tolerances: %+v", d.Tolerances)
	}
	for k, v := range want {
		if d.Tolerances[k] != v {
			t.Errorf("tolerance %s = %+v, want %+v", k, d.Tolerances[k], v)
		}
	}
}

func TestCPUFloor(t *testing.T) {
	cpu := func(b1, c, b2 float64) model.Finding {
		ps := passes(vals{100, 100, 100}, vals{})
		ps[0].Process.CPUSeconds, ps[1].Process.CPUSeconds, ps[2].Process.CPUSeconds = b1, c, b2
		return find(t, Classify(ps, DefaultThresholds()), "app.cpu_seconds")
	}
	// +0.3 s is 300% of the baseline but below the absolute floor.
	f := cpu(0.10, 0.40, 0.10)
	if f.Class != model.ClassNoise || !strings.Contains(f.Reason, "below the minimum of 0.50 s") {
		t.Errorf("small CPU rise: %q / %q", f.Class, f.Reason)
	}
	f = cpu(0.10, 0.80, 0.10)
	if f.Class != model.ClassRegression || !strings.Contains(f.Reason, "at least 0.50 s") {
		t.Errorf("large CPU rise: %q / %q", f.Class, f.Reason)
	}
}

// twoSteps builds passes with steps a then b on journey j. Counts and p95s
// are given per pass in the order B1, C, B2.
func twoSteps(countB [3]int, p95B [3]float64) []model.Pass {
	labels := []string{"B1", "C", "B2"}
	a95 := [3]float64{100, 300, 102} // step a regresses
	var ps []model.Pass
	for i, l := range labels {
		ps = append(ps, model.Pass{Label: l, Steps: []model.StepMetrics{
			{Journey: "j", Step: "a", Count: 50, P95: a95[i], P50: 20, Mean: 25},
			{Journey: "j", Step: "b", Count: countB[i], P95: p95B[i], P50: 20, Mean: 25},
		}})
	}
	return ps
}

func TestNotReached(t *testing.T) {
	// Step b was never reached on C: its latencies are 0, which must not read
	// as an improvement that turns step a's real regression into a trade-off.
	fs := Classify(twoSteps([3]int{50, 0, 50}, [3]float64{200, 0, 204}), DefaultThresholds())
	for _, m := range []string{model.MetricP95, model.MetricP50, model.MetricMean} {
		f := find(t, fs, "j.b."+m)
		if f.Class != model.ClassNotReached {
			t.Errorf("%s class = %q", m, f.Class)
		}
		if !strings.Contains(f.Reason, "pass C") || strings.Contains(f.Reason, "B1") {
			t.Errorf("%s reason = %q", m, f.Reason)
		}
	}
	if f := find(t, fs, "j.a.p95_ms"); f.Class != model.ClassRegression {
		t.Errorf("real regression class = %q (%s)", f.Class, f.Reason)
	}
	if f := find(t, fs, "j.b.failures"); f.Class != model.ClassNoise {
		t.Errorf("failures are still judged: %q", f.Class)
	}
}

func TestNotReachedNamesEveryEmptyPass(t *testing.T) {
	fs := Classify(twoSteps([3]int{0, 50, 0}, [3]float64{0, 5, 0}), DefaultThresholds())
	f := find(t, fs, "j.b.p95_ms")
	if f.Class != model.ClassNotReached || !strings.Contains(f.Reason, "passes B1, B2") {
		t.Errorf("got %q / %q", f.Class, f.Reason)
	}
}

func TestNotReachedFailuresStillJudged(t *testing.T) {
	ps := twoSteps([3]int{50, 0, 50}, [3]float64{200, 0, 204})
	ps[1].Steps[1].Failures = 3
	f := find(t, Classify(ps, DefaultThresholds()), "j.b.failures")
	if f.Class != model.ClassRegression {
		t.Errorf("failures class = %q", f.Class)
	}
}

func TestUnequalCounts(t *testing.T) {
	fs := Classify(twoSteps([3]int{50, 40, 48}, [3]float64{200, 201, 204}), DefaultThresholds())
	f := find(t, fs, "j.b.p95_ms")
	if f.Class != model.ClassNoise || !strings.Contains(f.Reason, "counts B1 50, C 40, B2 48") {
		t.Errorf("got %q / %q", f.Class, f.Reason)
	}
	if f := find(t, fs, "j.a.p95_ms"); strings.Contains(f.Reason, "counts") {
		t.Errorf("equal counts mentioned: %q", f.Reason)
	}
}

// proc is a process record with every counter set; the tests move one at a time.
func proc() model.ProcessStats {
	return model.ProcessStats{
		CPUSeconds: 1, PeakRSSMB: 50, CPUSecondsExact: 1, CPUSource: model.CPUSourceRusage,
		WriteBytes: 100000, WriteSyscalls: 400, OpenFDs: 7, Threads: 9, CompletedJourneys: 80,
		CPUMsPerJourney: 10, WriteBytesPerJourney: 1000, WriteSyscallsPerJourney: 5,
	}
}

func setMetric(p *model.ProcessStats, metric string, v float64) {
	switch metric {
	case model.MetricCPUPerJourney:
		p.CPUMsPerJourney = v
	case model.MetricWriteBytesPerJourney:
		p.WriteBytesPerJourney = v
	case model.MetricWriteSyscallsPerJourney:
		p.WriteSyscallsPerJourney = v
	case model.MetricOpenFDs:
		p.OpenFDs = int(v)
	case model.MetricThreads:
		p.Threads = int(v)
	case model.MetricRSS:
		p.PeakRSSMB = v
	}
}

func gatedPasses(metric string, v vals) []model.Pass {
	ps := passes(vals{100, 100, 100}, vals{})
	for i, x := range []float64{v.b1, v.c, v.b2} {
		ps[i].Process = proc()
		setMetric(&ps[i].Process, metric, x)
	}
	return ps
}

func TestGatedRule(t *testing.T) {
	cases := []struct {
		name       string
		metric     string
		v          vals
		class      string
		allowed    float64
		detectable float64
		word       string
	}{
		// cpu_ms_per_journey: relative 20% of the worse baseline
		{"regression", model.MetricCPUPerJourney, vals{10, 20, 10.5}, model.ClassRegression, 2.1, 2.1, "more than the allowed 2.10 ms/journey"},
		{"regression with noisy baselines", model.MetricCPUPerJourney, vals{10, 30, 15}, model.ClassRegression, 3, 5, "more than the noise band of 5.00 ms/journey"},
		{"rise within allowed is noise", model.MetricCPUPerJourney, vals{10, 11, 10.5}, model.ClassNoise, 2.1, 2.1, "not more than the allowed"},
		{"rise equal to allowed is noise", model.MetricCPUPerJourney, vals{10, 12, 10}, model.ClassNoise, 2, 2, "not more than the allowed"},
		{"inside the baselines", model.MetricCPUPerJourney, vals{10, 10.3, 10.5}, model.ClassNoise, 2.1, 2.1, "inside the range"},
		{"improved", model.MetricCPUPerJourney, vals{10, 5, 10.2}, model.ClassImproved, 2.04, 2.04, "better than the better baseline by 5.00"},
		{"small improvement is noise", model.MetricCPUPerJourney, vals{10, 9, 10.2}, model.ClassNoise, 2.04, 2.04, "not more than the allowed"},
		{"withheld: baselines disagree", model.MetricCPUPerJourney, vals{10, 16, 15}, model.ClassWithheld, 3, 5, "differ by 5.00 ms/journey, more than the allowed 3.00"},
		{"rise beyond allowed but inside band is withheld", model.MetricCPUPerJourney, vals{10, 17, 15}, model.ClassWithheld, 3, 5, "could not be seen"},
		{"improvement inside the band is withheld", model.MetricCPUPerJourney, vals{10, 6, 15}, model.ClassWithheld, 3, 5, "smallest change it could detect is 5.00"},
		// counters with a relative tolerance
		{"bytes regression", model.MetricWriteBytesPerJourney, vals{200000, 206000, 200800}, model.ClassRegression, 2008, 2008, "more than the allowed"},
		{"bytes noise", model.MetricWriteBytesPerJourney, vals{200000, 201600, 200800}, model.ClassNoise, 2008, 2008, "not more than the allowed"},
		{"bytes near zero under the 1 KB floor is noise", model.MetricWriteBytesPerJourney, vals{10, 12, 10}, model.ClassNoise, 1024, 1024, "not more than the allowed"},
		{"syscalls regression", model.MetricWriteSyscallsPerJourney, vals{5, 5.5, 5.1}, model.ClassRegression, 0.204, 0.204, "more than the allowed"},
		// peak memory
		{"memory regression", model.MetricRSS, vals{300, 318, 301}, model.ClassRegression, 15.05, 15.05, "more than the allowed"},
		{"memory noise", model.MetricRSS, vals{300, 315, 301}, model.ClassNoise, 15.05, 15.05, "not more than the allowed"},
		{"memory rise under the 10 MB floor is noise", model.MetricRSS, vals{5.7, 6.3, 5.9}, model.ClassNoise, 10, 10, "not more than the allowed"},
		// small integer counts use an absolute tolerance
		{"fds: one more is noise", model.MetricOpenFDs, vals{7, 8, 7}, model.ClassNoise, 1, 1, "an absolute tolerance"},
		{"fds: two more is a regression", model.MetricOpenFDs, vals{7, 9, 7}, model.ClassRegression, 1, 1, "more than the allowed 1"},
		{"fds: leak", model.MetricOpenFDs, vals{7, 207, 7}, model.ClassRegression, 1, 1, "more than the noise band of 0"},
		{"threads: 9 vs 10 is noise", model.MetricThreads, vals{9, 10, 10}, model.ClassNoise, 2, 2, "inside the range"},
		{"threads: two more is noise", model.MetricThreads, vals{9, 12, 10}, model.ClassNoise, 2, 2, "not more than the allowed 2"},
		{"threads: three more is a regression", model.MetricThreads, vals{9, 13, 10}, model.ClassRegression, 2, 2, "more than the allowed 2"},
		{"threads: baselines 4 apart are withheld", model.MetricThreads, vals{7, 12, 11}, model.ClassWithheld, 2, 4, "differ by 4"},
		{"threads: fewer threads is improved", model.MetricThreads, vals{20, 10, 20}, model.ClassImproved, 2, 2, "better than the better baseline by 10"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := Classify(gatedPasses(tc.metric, tc.v), DefaultThresholds())
			f := find(t, fs, "app."+tc.metric)
			if f.Class != tc.class {
				t.Fatalf("class = %q (%s), want %q", f.Class, f.Reason, tc.class)
			}
			if !strings.Contains(f.Reason, tc.word) {
				t.Errorf("reason %q does not contain %q", f.Reason, tc.word)
			}
			if !f.Gated || f.Journey != "" || f.Step != "" || f.Metric != tc.metric {
				t.Errorf("identity: %+v", f)
			}
			if !near6(f.Allowed, tc.allowed) || !near6(f.Detectable, tc.detectable) {
				t.Errorf("allowed %v detectable %v, want %v and %v", f.Allowed, f.Detectable, tc.allowed, tc.detectable)
			}
			worse := max(tc.v.b1, tc.v.b2)
			if !near6(f.Band, absf(tc.v.b1-tc.v.b2)) || !near6(f.Delta, tc.v.c-worse) {
				t.Errorf("band %v delta %v", f.Band, f.Delta)
			}
			if want := 100 * tc.detectable / worse; !near6(f.DetectablePct, want) {
				t.Errorf("detectable pct %v, want %v", f.DetectablePct, want)
			}
		})
	}
}

func near6(a, b float64) bool { return absf(a-b) < 1e-6 }

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestGatedKinds(t *testing.T) {
	fs := Classify(gatedPasses(model.MetricCPUPerJourney, vals{10, 10, 10}), DefaultThresholds())
	want := map[string]string{
		"app.cpu_ms_per_journey":         model.KindCPU,
		"app.write_bytes_per_journey":    model.KindCounter,
		"app.write_syscalls_per_journey": model.KindCounter,
		"app.open_fds":                   model.KindCounter,
		"app.threads":                    model.KindCounter,
		"app.peak_rss_mb":                model.KindMemory,
		"j.s.failures":                   model.KindFailures,
		"app.cpu_seconds":                model.KindLatency,
		"j.s.p95_ms":                     model.KindLatency,
		"j.s.p50_ms":                     model.KindLatency,
		"j.s.mean_ms":                    model.KindLatency,
	}
	if len(fs) != len(want) {
		t.Errorf("got %d findings, want %d", len(fs), len(want))
	}
	for id, kind := range want {
		f := find(t, fs, id)
		if f.Kind != kind || f.Gated != (kind != model.KindLatency) {
			t.Errorf("%s: kind %q gated %v, want kind %q", id, f.Kind, f.Gated, kind)
		}
	}
}

func TestGatedSteadyRunHasNoRegression(t *testing.T) {
	for _, f := range Classify(gatedPasses(model.MetricCPUPerJourney, vals{10, 10, 10}), DefaultThresholds()) {
		if f.Class != model.ClassNoise {
			t.Errorf("%s: %q on identical passes", f.ID, f.Class)
		}
	}
}

func TestNotMeasured(t *testing.T) {
	ps := gatedPasses(model.MetricOpenFDs, vals{7, 700, 7})
	ps[1].Process.Unmeasured = []string{model.UnmeasuredOpenFDs, model.UnmeasuredWriteBytesPerJourney}
	fs := Classify(ps, DefaultThresholds())
	f := find(t, fs, "app.open_fds")
	if f.Class != model.ClassNotMeasured || !strings.Contains(f.Reason, "pass C") {
		t.Errorf("open_fds: %q / %q", f.Class, f.Reason)
	}
	if !f.Gated || f.Kind != model.KindCounter {
		t.Errorf("not-measured keeps its kind: %+v", f)
	}
	if g := find(t, fs, "app.write_bytes_per_journey"); g.Class != model.ClassNotMeasured {
		t.Errorf("write bytes: %q", g.Class)
	}
	if g := find(t, fs, "app.threads"); g.Class != model.ClassNoise {
		t.Errorf("a measured counter was affected: %q", g.Class)
	}
	// two passes unread: both named
	ps[0].Process.Unmeasured = []string{model.UnmeasuredOpenFDs}
	f = find(t, Classify(ps, DefaultThresholds()), "app.open_fds")
	if !strings.Contains(f.Reason, "passes B1, C") {
		t.Errorf("reason = %q", f.Reason)
	}
}

func TestNeverRegressionWhenNotJudged(t *testing.T) {
	// A step that was never reached and a counter that was never read can
	// have any figures; neither may be a regression.
	ps := twoSteps([3]int{50, 0, 50}, [3]float64{200, 9000, 204})
	for i := range ps {
		ps[i].Process = proc()
	}
	ps[1].Process.Unmeasured = []string{model.UnmeasuredCPUPerJourney}
	ps[1].Process.CPUMsPerJourney = 5000
	for _, f := range Classify(ps, DefaultThresholds()) {
		if f.ID == "j.b.p95_ms" && f.Class != model.ClassNotReached {
			t.Errorf("%s: %q", f.ID, f.Class)
		}
		if f.ID == "app.cpu_ms_per_journey" && f.Class != model.ClassNotMeasured {
			t.Errorf("%s: %q", f.ID, f.Class)
		}
	}
}

func TestGatedTradeOff(t *testing.T) {
	ps := gatedPasses(model.MetricCPUPerJourney, vals{10, 13, 10.2})
	for i, th := range []int{30, 10, 30} {
		ps[i].Process.Threads = th
	}
	// CPU per journey is 27% above the worse baseline; threads are 67% below the better one.
	fs := Classify(ps, DefaultThresholds())
	for _, id := range []string{"app.cpu_ms_per_journey", "app.threads"} {
		f := find(t, fs, id)
		if f.Class != model.ClassTradeOff || !strings.Contains(f.Reason, "clearly better and another clearly worse") {
			t.Errorf("%s: %q %q", id, f.Class, f.Reason)
		}
	}
	if f := find(t, fs, "app.peak_rss_mb"); f.Class != model.ClassNoise {
		t.Errorf("unrelated metric re-classed: %q", f.Class)
	}
}

// A small improvement must not hide a large regression.
func TestSmallImprovementDoesNotExcuseLargeRegression(t *testing.T) {
	ps := gatedPasses(model.MetricCPUPerJourney, vals{10, 100, 10.2})
	for i, v := range []float64{5, 4.5, 5} { // syscalls improve by 10%
		ps[i].Process.WriteSyscallsPerJourney = v
	}
	fs := Classify(ps, DefaultThresholds())
	if f := find(t, fs, "app.write_syscalls_per_journey"); f.Class != model.ClassImproved {
		t.Errorf("syscalls: %q (%s)", f.Class, f.Reason)
	}
	if f := find(t, fs, "app.cpu_ms_per_journey"); f.Class != model.ClassRegression {
		t.Errorf("cpu: %q (%s)", f.Class, f.Reason)
	}
	// a regression from a zero baseline has no finite size and is never excused
	ps = gatedPasses(model.MetricOpenFDs, vals{0, 5, 0})
	for i, th := range []int{30, 10, 30} {
		ps[i].Process.Threads = th
	}
	if f := find(t, Classify(ps, DefaultThresholds()), "app.open_fds"); f.Class != model.ClassRegression {
		t.Errorf("fds from zero: %q", f.Class)
	}
}

func TestLegacyPassesHaveNoCounterFindings(t *testing.T) {
	ps := passes(vals{100, 100, 100}, vals{}) // no CPU source: a run before the counters
	for _, f := range Classify(ps, DefaultThresholds()) {
		switch f.Metric {
		case model.MetricCPUPerJourney, model.MetricWriteBytesPerJourney, model.MetricWriteSyscallsPerJourney, model.MetricOpenFDs, model.MetricThreads:
			t.Errorf("counter finding %s for passes that never recorded counters", f.ID)
		}
	}
}

func TestTolerancesFallBackToDefaults(t *testing.T) {
	th := DefaultThresholds()
	th.Tolerances = nil
	f := find(t, Classify(gatedPasses(model.MetricOpenFDs, vals{7, 9, 7}), th), "app.open_fds")
	if f.Class != model.ClassRegression {
		t.Errorf("class = %q", f.Class)
	}
	// a configured tolerance wins
	th.Tolerances = map[string]model.Tolerance{model.MetricOpenFDs: {Absolute: 5}}
	f = find(t, Classify(gatedPasses(model.MetricOpenFDs, vals{7, 9, 7}), th), "app.open_fds")
	if f.Class != model.ClassNoise || f.Allowed != 5 {
		t.Errorf("class = %q allowed %v", f.Class, f.Allowed)
	}
}

func TestLatencyDetectable(t *testing.T) {
	cases := []struct {
		name string
		p95  vals
		want float64
		pct  float64
	}{
		{"floor", vals{100, 100, 100}, 20, 20}, // 20% of 100 exceeds the floor of 10 ms and twice the band
		{"twice the band", vals{100, 100, 130}, 60, 60.0 / 130 * 100},
		{"absolute floor", vals{2, 2, 2.2}, 10, 10 / 2.2 * 100},
	}
	for _, tc := range cases {
		f := find(t, Classify(passes(tc.p95, vals{}), DefaultThresholds()), "j.s.p95_ms")
		if !near6(f.Detectable, tc.want) || !near6(f.DetectablePct, tc.pct) {
			t.Errorf("%s: detectable %v (%v%%), want %v (%v%%)", tc.name, f.Detectable, f.DetectablePct, tc.want, tc.pct)
		}
	}
	// failures: one failure is detectable when both baselines were clean
	fs := Classify(passes(vals{100, 100, 100}, vals{}), DefaultThresholds())
	if f := find(t, fs, "j.s.failures"); f.Detectable != 1 || !f.Gated {
		t.Errorf("failures: %+v", f)
	}
	fs = Classify(passes(vals{100, 100, 100}, vals{0, 3, 2}), DefaultThresholds())
	if f := find(t, fs, "j.s.failures"); f.Detectable != 0 {
		t.Errorf("failures with a failing baseline: %+v", f)
	}
}

// Identical code in B1, C and B2 must never give a gated regression. The A/A
// runs in evidence/aa are replayed through the classifier as committed.
func TestReplayAARuns(t *testing.T) {
	// "runtime" carries the Go runtime counters in its process JSON; the
	// parser reads only the keys it knows and ignores the rest, so every set
	// replays through the same code.
	for _, set := range []string{"before", "counters", "after", "runtime"} {
		t.Run(set, func(t *testing.T) {
			runs := readPasses(t, "../../evidence/aa/"+set+"/passes.tsv")
			if len(runs) == 0 {
				t.Fatal("no runs read")
			}
			names := make([]string, 0, len(runs))
			for n := range runs {
				names = append(names, n)
			}
			sort.Strings(names)
			gatedFindings, runtimeFindings := 0, 0
			for _, n := range names {
				ps := runs[n]
				if len(ps) != 3 {
					t.Fatalf("%s: %d passes", n, len(ps))
				}
				for _, f := range Classify(ps, DefaultThresholds()) {
					if f.Gated {
						gatedFindings++
						switch f.Metric {
						case model.MetricAllocObjectsPerJourney, model.MetricAllocBytesPerJourney, model.MetricGCCycles, model.MetricGoroutines:
							runtimeFindings++
						}
					}
					if f.Gated && f.Class == model.ClassRegression {
						t.Errorf("%s: gated regression %s: B1 %v C %v B2 %v (%s)", n, f.ID, f.B1, f.C, f.B2, f.Reason)
					}
				}
			}
			if gatedFindings == 0 {
				t.Errorf("no gated finding was produced")
			}
			if want := 4 * len(names); set == "runtime" && runtimeFindings != want {
				t.Errorf("runtime set: %d runtime-counter findings, want %d", runtimeFindings, want)
			}
			t.Logf("%s: %d runs replayed, %d gated findings", set, len(names), gatedFindings)
		})
	}
}

// readPasses rebuilds the passes of every run in a passes.tsv written by
// scripts/aa_summary.py, keyed by run name.
func readPasses(t *testing.T, path string) map[string][]model.Pass {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	byRun := map[string]map[string]*model.Pass{}
	order := map[string][]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) != 10 {
			t.Fatalf("%s: %d columns in %q", path, len(cols), sc.Text())
		}
		run, label, journey, step := cols[0], cols[1], cols[2], cols[3]
		if byRun[run] == nil {
			byRun[run] = map[string]*model.Pass{}
		}
		p := byRun[run][label]
		if p == nil {
			p = &model.Pass{Label: label}
			byRun[run][label] = p
			order[run] = append(order[run], label)
		}
		if journey == "app" {
			if err := json.Unmarshal([]byte(cols[9]), &p.Process); err != nil {
				t.Fatalf("%s %s %s: %v", path, run, label, err)
			}
			// Sets written before the total GC count have only the per-journey
			// figure, which is that total over the completed journeys.
			if p.Process.GCCycles == 0 && p.Process.GCCyclesPerJourney > 0 {
				p.Process.GCCycles = math.Round(p.Process.GCCyclesPerJourney * float64(p.Process.CompletedJourneys))
			}
			continue
		}
		num := func(s string) float64 {
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				t.Fatalf("%s: %q: %v", path, s, err)
			}
			return v
		}
		p.Steps = append(p.Steps, model.StepMetrics{
			Journey: journey, Step: step, Count: int(num(cols[4])), Failures: int(num(cols[5])),
			P50: num(cols[6]), P95: num(cols[7]), Mean: num(cols[8]),
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	out := map[string][]model.Pass{}
	for run, labels := range order {
		for _, l := range labels {
			out[run] = append(out[run], *byRun[run][l])
		}
	}
	return out
}

// runtimeProc is proc with the Go runtime counters set.
func runtimeProc() model.ProcessStats {
	p := proc()
	p.AllocObjectsPerJourney, p.AllocBytesPerJourney, p.GCCycles, p.GCCyclesPerJourney, p.Goroutines = 200, 20000, 0, 0, 7
	return p
}

func runtimePasses(metric string, v vals) []model.Pass {
	ps := passes(vals{100, 100, 100}, vals{})
	for i, x := range []float64{v.b1, v.c, v.b2} {
		ps[i].Process = runtimeProc()
		switch metric {
		case model.MetricAllocObjectsPerJourney:
			ps[i].Process.AllocObjectsPerJourney = x
		case model.MetricAllocBytesPerJourney:
			ps[i].Process.AllocBytesPerJourney = x
		case model.MetricGCCycles:
			ps[i].Process.GCCycles = x
			ps[i].Process.GCCyclesPerJourney = x / float64(ps[i].Process.CompletedJourneys)
		case model.MetricGoroutines:
			ps[i].Process.Goroutines = int(x)
		}
	}
	return ps
}

func TestRuntimeCounterRule(t *testing.T) {
	cases := []struct {
		name   string
		metric string
		v      vals
		class  string
		unit   string
	}{
		{"objects: 8% is noise", model.MetricAllocObjectsPerJourney, vals{2000, 2160, 2020}, model.ClassNoise, "objects/journey"},
		{"objects: 15% is a regression", model.MetricAllocObjectsPerJourney, vals{2000, 2320, 2020}, model.ClassRegression, "objects/journey"},
		{"objects: 15% under the 100-object floor is noise", model.MetricAllocObjectsPerJourney, vals{200, 232, 202}, model.ClassNoise, "objects/journey"},
		{"bytes: 3% is noise", model.MetricAllocBytesPerJourney, vals{200000, 207000, 201000}, model.ClassNoise, "B/journey"},
		{"bytes: 10% is a regression", model.MetricAllocBytesPerJourney, vals{200000, 223000, 201000}, model.ClassRegression, "B/journey"},
		{"bytes: 10% under the 16 KB floor is noise", model.MetricAllocBytesPerJourney, vals{20000, 22300, 20100}, model.ClassNoise, "B/journey"},
		{"gc: no cycle in any pass is noise", model.MetricGCCycles, vals{0, 0, 0}, model.ClassNoise, "cycles"},
		{"gc: one extra cycle is noise", model.MetricGCCycles, vals{0, 1, 0}, model.ClassNoise, "cycles"},
		{"gc: two extra cycles are inside the tolerance", model.MetricGCCycles, vals{0, 2, 0}, model.ClassNoise, "cycles"},
		{"gc: three extra cycles are a regression", model.MetricGCCycles, vals{0, 3, 0}, model.ClassRegression, "cycles"},
		{"goroutines: one more is noise", model.MetricGoroutines, vals{7, 8, 7}, model.ClassNoise, "count"},
		{"goroutines: a leak is a regression", model.MetricGoroutines, vals{7, 207, 7}, model.ClassRegression, "count"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := find(t, Classify(runtimePasses(tc.metric, tc.v), DefaultThresholds()), "app."+tc.metric)
			if f.Class != tc.class || f.Unit != tc.unit || f.Kind != model.KindCounter || !f.Gated {
				t.Errorf("%+v", f)
			}
		})
	}
}

func TestRuntimeCountersAbsentFromOlderRuns(t *testing.T) {
	// proc() has no goroutine count and no Unmeasured entry: a run.json from
	// before the runtime counters existed.
	fs := Classify(gatedPasses(model.MetricThreads, vals{9, 9, 9}), DefaultThresholds())
	for _, m := range []string{model.MetricAllocObjectsPerJourney, model.MetricAllocBytesPerJourney, model.MetricGCCycles, model.MetricGoroutines} {
		for _, f := range fs {
			if f.Metric == m {
				t.Errorf("%s judged for a run without runtime counters: %+v", m, f)
			}
		}
	}
}

func TestRuntimeCountersNotMeasured(t *testing.T) {
	ps := runtimePasses(model.MetricGoroutines, vals{7, 7, 7})
	for i := range ps {
		ps[i].Process.AllocObjectsPerJourney, ps[i].Process.AllocBytesPerJourney, ps[i].Process.GCCycles, ps[i].Process.GCCyclesPerJourney, ps[i].Process.Goroutines = 0, 0, 0, 0, 0
		ps[i].Process.Unmeasured = []string{"alloc_objects_per_journey", "alloc_bytes_per_journey", "gc_cycles", "gc_cycles_per_journey", "goroutines"}
	}
	fs := Classify(ps, DefaultThresholds())
	for _, m := range []string{model.MetricAllocObjectsPerJourney, model.MetricAllocBytesPerJourney, model.MetricGCCycles, model.MetricGoroutines} {
		f := find(t, fs, "app."+m)
		if f.Class != model.ClassNotMeasured || !f.Gated {
			t.Errorf("%s: %+v", m, f)
		}
	}
}

// The GC tolerance is on the pass total, so it means the same at any journey
// count: two extra cycles are noise and three are a regression whether the
// pass completed 20 journeys or 20000.
func TestGCCyclesToleranceDoesNotDependOnJourneyCount(t *testing.T) {
	for _, completed := range []int{20, 80, 20000} {
		for _, tc := range []struct {
			extra float64
			class string
		}{{2, model.ClassNoise}, {3, model.ClassRegression}} {
			ps := runtimePasses(model.MetricGCCycles, vals{1, 1 + tc.extra, 1})
			for i := range ps {
				ps[i].Process.CompletedJourneys = completed
				ps[i].Process.GCCyclesPerJourney = ps[i].Process.GCCycles / float64(completed)
			}
			f := find(t, Classify(ps, DefaultThresholds()), "app.gc_cycles")
			if f.Class != tc.class {
				t.Errorf("%d journeys, %v extra cycles: class %s, want %s (%s)", completed, tc.extra, f.Class, tc.class, f.Reason)
			}
		}
	}
}

// A run.json written between the per-journey GC figure and the total has
// gc_cycles_per_journey but no gc_cycles: nothing is compared for it.
func TestGCCyclesAbsentFromRunsWithOnlyPerJourney(t *testing.T) {
	ps := runtimePasses(model.MetricGoroutines, vals{7, 7, 7})
	for i := range ps {
		ps[i].Process.GCCycles, ps[i].Process.GCCyclesPerJourney = 0, 0.05
	}
	for _, f := range Classify(ps, DefaultThresholds()) {
		if f.Metric == model.MetricGCCycles || f.Metric == model.MetricGCCyclesPerJourney {
			t.Errorf("GC judged without a total: %+v", f)
		}
	}
	// and a pass from that era that lists only the old name as unread
	for i := range ps {
		ps[i].Process.GCCyclesPerJourney = 0
		ps[i].Process.Unmeasured = []string{"gc_cycles_per_journey"}
	}
	f := find(t, Classify(ps, DefaultThresholds()), "app.gc_cycles")
	if f.Class != model.ClassNotMeasured {
		t.Errorf("old Unmeasured name not honoured: %+v", f)
	}
}

// CPU per journey is compared only when B1, C and B2 were read the same way:
// a window figure and a lifetime figure differ by the start-up cost.
func TestCPUSourceMismatchIsNotMeasured(t *testing.T) {
	mk := func(sources ...string) []model.Pass {
		ps := passes(vals{100, 100, 100}, vals{})
		for i := range ps {
			ps[i].Process = proc()
			ps[i].Process.CPUSource = sources[i]
		}
		ps[1].Process.CPUMsPerJourney = 500 // would be a regression if compared
		return ps
	}
	f := find(t, Classify(mk(model.CPUSourceSchedstat, model.CPUSourceRusage, model.CPUSourceSchedstat), DefaultThresholds()), "app.cpu_ms_per_journey")
	if f.Class != model.ClassNotMeasured || !f.Gated || !strings.Contains(f.Reason, "B1 schedstat, C rusage, B2 schedstat") {
		t.Errorf("mismatched sources: %+v", f)
	}
	f = find(t, Classify(mk(model.CPUSourceSchedstat, model.CPUSourceSchedstat, model.CPUSourceSchedstat), DefaultThresholds()), "app.cpu_ms_per_journey")
	if f.Class != model.ClassRegression {
		t.Errorf("same source not compared: %+v", f)
	}
	// the other gated counters are still compared when only the CPU source differs
	fs := Classify(mk(model.CPUSourceSchedstat, model.CPUSourceRusage, model.CPUSourceSchedstat), DefaultThresholds())
	if f := find(t, fs, "app.threads"); f.Class == model.ClassNotMeasured {
		t.Errorf("threads dropped on a CPU source mismatch: %+v", f)
	}
}
