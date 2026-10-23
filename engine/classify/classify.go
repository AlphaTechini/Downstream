// Package classify turns the three passes (B1, C, B2) into findings.
//
// Every metric is "higher is worse". The noise band is the difference between
// the two baseline runs; the candidate is judged against the worse baseline.
//
// Two rules apply, by the metric's kind:
//
// Gated metrics (counters, CPU per completed journey, peak memory, step
// failures) decide whether a run has a regression. For a counter, CPU or memory
// metric, with worse = max(B1, B2), better = min(B1, B2), band = |B1 - B2| and
// allowed = max(Relative * worse, Absolute) from the metric's Tolerance:
//
//	regression: C - worse > allowed and C - worse > band
//	improved:   the mirror, better - C against allowed taken on better, and band
//	withheld:   neither of the above, and band > allowed: the two baselines of
//	            this run disagree by more than the tolerance, so a change of the
//	            tolerance's size could not be seen
//	noise:      otherwise
//
// The smallest change this run could detect is max(allowed, band). A change
// beyond it is a regression even when the baselines are noisy, so a large
// regression is never hidden by noisy baselines. A step failure is a
// regression when the step passed on both baselines and failed on the
// candidate.
//
// Latency metrics (p50, p95, mean per step, and the app's total CPU seconds,
// which depends on how many journeys completed) are advisory: they get a class
// by the older band, relative and floor rule so a report can say "slower" or
// "faster", but they never count toward a verdict and never open an issue.
package classify

import (
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	"downstream/engine/model"
)

// DefaultThresholds are the rules in effect.
//
// The latency fields are the older advisory rule from agentnotes.md.
//
// The tolerances come from A/A runs, where the same commit is B1, C and B2 and
// any difference is noise (evidence/aa/counters/summary.txt, lines 14 to 19:
// the largest excess of C over the worse baseline, per metric). The rule used
// to pick each: about 1.5 to 2 times that largest excess, rounded up; for the
// small integer counts (open fds, threads) an absolute tolerance, because one
// thread is a large fraction of ten.
func DefaultThresholds() model.Thresholds {
	return model.Thresholds{
		BandMultiplier:    2,
		MinRelative:       0.20,
		MinLatencyMS:      10,
		MinMemoryMB:       10,
		MinCPUSeconds:     0.5,
		MaxBaselineSpread: 0.5,
		Tolerances: map[string]model.Tolerance{
			model.MetricWriteBytesPerJourney:    {Relative: 0.01},
			model.MetricWriteSyscallsPerJourney: {Relative: 0.04},
			model.MetricOpenFDs:                 {Absolute: 1},
			model.MetricThreads:                 {Absolute: 2},
			model.MetricCPUPerJourney:           {Relative: 0.20},
			model.MetricRSS:                     {Relative: 0.02},
		},
	}
}

// gatedMetric describes one process figure judged by the gated rule.
type gatedMetric struct {
	metric string
	kind   string
	unit   string
	get    func(model.ProcessStats) float64
	// needsCounters is true for figures that exist only in a run.json written
	// after the process counters were added.
	needsCounters bool
}

var gatedMetrics = []gatedMetric{
	{model.MetricCPUPerJourney, model.KindCPU, "ms/journey", func(p model.ProcessStats) float64 { return p.CPUMsPerJourney }, true},
	{model.MetricWriteBytesPerJourney, model.KindCounter, "B/journey", func(p model.ProcessStats) float64 { return p.WriteBytesPerJourney }, true},
	{model.MetricWriteSyscallsPerJourney, model.KindCounter, "calls/journey", func(p model.ProcessStats) float64 { return p.WriteSyscallsPerJourney }, true},
	{model.MetricOpenFDs, model.KindCounter, "count", func(p model.ProcessStats) float64 { return float64(p.OpenFDs) }, true},
	{model.MetricThreads, model.KindCounter, "count", func(p model.ProcessStats) float64 { return float64(p.Threads) }, true},
	{model.MetricRSS, model.KindMemory, "MB", func(p model.ProcessStats) float64 { return p.PeakRSSMB }, false},
}

type key struct{ journey, step string }

// Classify compares C with B1 and B2. It returns nil when any of the three
// passes is missing, because no comparison can be made without all of them.
func Classify(passes []model.Pass, t model.Thresholds) []model.Finding {
	var b1, c, b2 *model.Pass
	for i := range passes {
		switch passes[i].Label {
		case "B1":
			b1 = &passes[i]
		case "C":
			c = &passes[i]
		case "B2":
			b2 = &passes[i]
		}
	}
	if b1 == nil || c == nil || b2 == nil {
		return nil
	}

	idx1, idxC, idx2 := index(b1), index(c), index(b2)
	var out []model.Finding
	counts := map[key][3]int{}
	for k, mc := range idxC {
		m1, ok1 := idx1[k]
		m2, ok2 := idx2[k]
		if !ok1 || !ok2 {
			continue
		}
		id := k.journey + "." + k.step + "."
		counts[k] = [3]int{m1.Count, mc.Count, m2.Count}
		latency := func(metric string, v1, vc, v2 float64) {
			f := judge(v1, vc, v2, "ms", t)
			if m1.Count == 0 || mc.Count == 0 || m2.Count == 0 {
				f.Class = model.ClassNotReached
				f.Reason = notReachedReason(m1.Count, mc.Count, m2.Count)
			}
			f.ID = id + metric
			f.Journey, f.Step, f.Metric, f.Unit = k.journey, k.step, metric, "ms"
			f.Kind = model.KindLatency
			out = append(out, f)
		}
		latency(model.MetricP95, m1.P95, mc.P95, m2.P95)
		latency(model.MetricP50, m1.P50, mc.P50, m2.P50)
		latency(model.MetricMean, m1.Mean, mc.Mean, m2.Mean)

		ff := judge(float64(m1.Failures), float64(mc.Failures), float64(m2.Failures), "count", t)
		ff.ID = id + model.MetricFailures
		ff.Journey, ff.Step, ff.Metric, ff.Unit = k.journey, k.step, model.MetricFailures, "count"
		ff.Kind, ff.Gated = model.KindFailures, true
		out = append(out, ff)
	}

	cpu := judge(b1.Process.CPUSeconds, c.Process.CPUSeconds, b2.Process.CPUSeconds, "s", t)
	cpu.ID = "app." + model.MetricCPU
	cpu.Metric, cpu.Unit, cpu.Kind = model.MetricCPU, "s", model.KindLatency
	out = append(out, cpu)

	haveCounters := b1.Process.CPUSource != "" && c.Process.CPUSource != "" && b2.Process.CPUSource != ""
	for _, m := range gatedMetrics {
		if m.needsCounters && !haveCounters {
			continue
		}
		var f model.Finding
		if missing := unmeasuredIn(m.metric, b1, c, b2); len(missing) > 0 {
			f = model.Finding{
				Class:  model.ClassNotMeasured,
				Reason: "not measured in " + passWord(missing) + " " + strings.Join(missing, ", ") + ": the figure could not be read, so nothing is compared",
			}
		} else {
			f = judgeGated(m.get(b1.Process), m.get(c.Process), m.get(b2.Process), m.unit, tolerance(t, m.metric))
		}
		f.ID = "app." + m.metric
		f.Metric, f.Unit, f.Kind, f.Gated = m.metric, m.unit, m.kind, true
		out = append(out, f)
	}

	tradeOffs(out)
	noteUnequalCounts(out, counts)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func index(p *model.Pass) map[key]model.StepMetrics {
	m := make(map[key]model.StepMetrics, len(p.Steps))
	for _, s := range p.Steps {
		m[key{s.Journey, s.Step}] = s
	}
	return m
}

// tolerance returns the metric's tolerance from t, or the default when t has
// none for it (thresholds built by hand, without a Tolerances map).
func tolerance(t model.Thresholds, metric string) model.Tolerance {
	if tol, ok := t.Tolerances[metric]; ok {
		return tol
	}
	return DefaultThresholds().Tolerances[metric]
}

// unmeasuredIn returns the labels of the passes that list metric as unread.
func unmeasuredIn(metric string, b1, c, b2 *model.Pass) []string {
	var out []string
	for _, p := range []struct {
		label string
		pass  *model.Pass
	}{{"B1", b1}, {"C", c}, {"B2", b2}} {
		if slices.Contains(p.pass.Process.Unmeasured, metric) {
			out = append(out, p.label)
		}
	}
	return out
}

func passWord(labels []string) string {
	if len(labels) > 1 {
		return "passes"
	}
	return "pass"
}

// judgeGated fills every Finding field except ID, Journey, Step, Metric, Unit,
// Kind and Gated, by the gated rule in the package comment.
func judgeGated(v1, vc, v2 float64, unit string, tol model.Tolerance) model.Finding {
	band := math.Abs(v1 - v2)
	worse, better := math.Max(v1, v2), math.Min(v1, v2)
	allowed := math.Max(tol.Relative*worse, tol.Absolute)
	f := model.Finding{
		B1: v1, C: vc, B2: v2, Band: band, Delta: vc - worse, Allowed: allowed,
		Detectable: math.Max(allowed, band),
	}
	if worse != 0 {
		f.DeltaPct = 100 * f.Delta / worse
		f.DetectablePct = 100 * f.Detectable / worse
	}
	allowedWords := amount(allowed, unit) + " (" + toleranceWords(tol) + ")"
	bandWords := "the noise band of " + amount(band, unit)

	if f.Delta > allowed && f.Delta > band {
		f.Class = model.ClassRegression
		f.Reason = "worse than the worse baseline by " + amount(f.Delta, unit) + " (" + num(f.DeltaPct, 1) +
			"%), more than the allowed " + allowedWords + " and more than " + bandWords
		return f
	}

	gain := better - vc
	allowedBetter := math.Max(tol.Relative*better, tol.Absolute)
	if gain > allowedBetter && gain > band {
		f.Class = model.ClassImproved
		f.Reason = "better than the better baseline by " + amount(gain, unit) +
			", more than the allowed " + amount(allowedBetter, unit) + " (" + toleranceWords(tol) + ") and more than " + bandWords
		return f
	}

	if band > allowed {
		f.Class = model.ClassWithheld
		f.Reason = "the two baseline runs differ by " + amount(band, unit) + ", more than the allowed " + allowedWords +
			", so a change of the allowed size could not be seen in this run; the smallest change it could detect is " +
			amount(f.Detectable, unit)
		if f.DetectablePct != 0 {
			f.Reason += " (" + num(f.DetectablePct, 1) + "% of the worse baseline)"
		}
		return f
	}

	f.Class = model.ClassNoise
	switch {
	case f.Delta > 0:
		d := "worse than the worse baseline by " + amount(f.Delta, unit)
		if !(f.Delta > allowed) {
			f.Reason = d + ", not more than the allowed " + allowedWords
		} else {
			f.Reason = d + ", not more than " + bandWords
		}
	case vc >= better:
		f.Reason = "inside the range of the two baseline runs (" + amount(better, unit) + " to " + amount(worse, unit) + ")"
	default:
		d := "better than the better baseline by " + amount(gain, unit)
		if !(gain > allowedBetter) {
			f.Reason = d + ", not more than the allowed " + amount(allowedBetter, unit) + " (" + toleranceWords(tol) + ")"
		} else {
			f.Reason = d + ", not more than " + bandWords
		}
	}
	return f
}

// toleranceWords says how a tolerance is defined.
func toleranceWords(t model.Tolerance) string {
	rel := pctWords(t.Relative) + " of the worse baseline"
	switch {
	case t.Relative > 0 && t.Absolute > 0:
		return "the larger of " + rel + " and an absolute " + num(t.Absolute, -1)
	case t.Absolute > 0:
		return "an absolute tolerance"
	}
	return rel
}

// pctWords prints a fraction as a percentage without trailing zeros.
func pctWords(frac float64) string {
	s := strconv.FormatFloat(100*frac, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")
	return s + "%"
}

// judge applies the advisory latency rule (and, for unit "count", the failures
// rule). It fills every Finding field except ID, Journey, Step, Metric, Unit,
// Kind and Gated.
func judge(v1, vc, v2 float64, unit string, t model.Thresholds) model.Finding {
	band := math.Abs(v1 - v2)
	worse, better := math.Max(v1, v2), math.Min(v1, v2)
	f := model.Finding{B1: v1, C: vc, B2: v2, Band: band, Delta: vc - worse}
	if worse != 0 {
		f.DeltaPct = 100 * f.Delta / worse
	}

	if unit == "count" {
		f.Class, f.Reason = judgeFailures(v1, vc, v2)
		// One new failure is enough, so one is the smallest change it can see;
		// when a baseline already failed, no new failure can be told apart.
		if worse == 0 {
			f.Detectable = 1
		}
		return f
	}

	floor, floorWords := 0.0, ""
	switch unit {
	case "ms":
		floor = t.MinLatencyMS
	case "MB":
		floor = t.MinMemoryMB
	case "s":
		floor = t.MinCPUSeconds
	}
	// The smallest rise the rule below could call a regression.
	f.Detectable = math.Max(t.BandMultiplier*band, math.Max(floor, t.MinRelative*worse))
	if worse != 0 {
		f.DetectablePct = 100 * f.Detectable / worse
	}
	if floor > 0 {
		floorWords = " and at least " + amount(floor, unit)
	}
	bandWords := num(t.BandMultiplier, -1) + " times the noise band of " + amount(band, unit)
	relWords := num(t.MinRelative*100, 1) + "%"
	relWords = trimZero(relWords)

	if f.Delta > t.BandMultiplier*band && f.DeltaPct >= t.MinRelative*100 && f.Delta >= floor {
		f.Class = model.ClassRegression
		f.Reason = "worse than the worse baseline by " + amount(f.Delta, unit) +
			" (" + num(f.DeltaPct, 1) + "%), which is more than " + bandWords +
			", at least " + relWords + floorWords
		return f
	}

	gain := better - vc
	if gain > t.BandMultiplier*band && better > 0 && gain/better >= t.MinRelative && gain >= floor {
		f.Class = model.ClassImproved
		f.Reason = "better than the better baseline by " + amount(gain, unit) +
			" (" + num(100*gain/better, 1) + "%), which is more than " + bandWords +
			", at least " + relWords + floorWords
		return f
	}

	// The spread rule only applies when the candidate is neither clearly
	// worse nor clearly better than both baselines: a gap that large is
	// visible however noisy the baselines are. A spread below the absolute
	// floor is jitter on a fast step, so it does not stop a verdict.
	if better > 0 && band/better > t.MaxBaselineSpread && band >= floor {
		f.Class = model.ClassWithheld
		f.Reason = "baseline spread rule: the two baseline runs differ by " +
			num(band/better, 2) + " of the smaller one, above the limit of " +
			num(t.MaxBaselineSpread, -1) + ", so no verdict is given"
		return f
	}

	f.Class = model.ClassNoise
	switch {
	case f.Delta > 0:
		d := "worse than the worse baseline by " + amount(f.Delta, unit)
		switch {
		case !(f.Delta > t.BandMultiplier*band):
			f.Reason = d + ", which is not more than " + bandWords
		case !(f.DeltaPct >= t.MinRelative*100):
			f.Reason = d + " (" + num(f.DeltaPct, 1) + "%), below the minimum of " + relWords
		default:
			f.Reason = d + ", below the minimum of " + amount(floor, unit)
		}
	case vc >= better:
		f.Reason = "inside the range of the two baseline runs (" + amount(better, unit) +
			" to " + amount(worse, unit) + ")"
	default:
		d := "better than the better baseline by " + amount(gain, unit)
		switch {
		case !(gain > t.BandMultiplier*band):
			f.Reason = d + ", which is not more than " + bandWords
		case !(better > 0 && gain/better >= t.MinRelative):
			f.Reason = d + ", below the minimum of " + relWords
		default:
			f.Reason = d + ", below the minimum of " + amount(floor, unit)
		}
	}
	return f
}

func judgeFailures(b1, c, b2 float64) (string, string) {
	if b1 > 0 || b2 > 0 {
		return model.ClassNoise, "failed on the baseline too"
	}
	if c > 0 {
		times := " times"
		if c == 1 {
			times = " time"
		}
		return model.ClassRegression, "passed on both baseline runs, failed " + num(c, 0) + times + " on the candidate"
	}
	return model.ClassNoise, "no failures on the baseline runs or the candidate"
}

// tradeOffs re-classes app-wide gated findings that offset each other. A
// regression is a trade-off only when some gated metric improved by at least as
// large a fraction of its own baseline as the regression rose, so a small
// improvement never excuses a large regression. Each improvement that excuses a
// regression is re-classed with it. Step failures stay what they are, and
// advisory latency findings are never re-classed.
func tradeOffs(fs []model.Finding) {
	rise := func(f model.Finding) float64 { // fraction above the worse baseline
		worse := math.Max(f.B1, f.B2)
		if worse <= 0 {
			return math.Inf(1)
		}
		return f.Delta / worse
	}
	gain := func(f model.Finding) float64 { // fraction below the better baseline
		better := math.Min(f.B1, f.B2)
		if better <= 0 {
			return 0
		}
		return (better - f.C) / better
	}
	var excused, excusing []int
	for i := range fs {
		if !appGated(fs[i]) || fs[i].Class != model.ClassRegression {
			continue
		}
		for j := range fs {
			if appGated(fs[j]) && fs[j].Class == model.ClassImproved && gain(fs[j]) >= rise(fs[i]) {
				excused = append(excused, i)
				excusing = append(excusing, j)
			}
		}
	}
	for _, i := range append(excused, excusing...) {
		fs[i].Class = model.ClassTradeOff
		fs[i].Reason = "one gated metric clearly better and another clearly worse, the improvement at least as large a fraction of its baseline as the regression"
	}
}

func appGated(f model.Finding) bool {
	return f.Gated && f.Journey == "" && f.Kind != model.KindFailures
}

func isLatency(f model.Finding) bool {
	return f.Kind == model.KindLatency && f.Journey != ""
}

func num(v float64, dec int) string { return strconv.FormatFloat(v, 'f', dec, 64) }

func trimZero(s string) string {
	if len(s) > 3 && s[len(s)-3:] == ".0%" {
		return s[:len(s)-3] + "%"
	}
	return s
}

func amount(v float64, unit string) string {
	switch unit {
	case "ms", "MB":
		return num(v, 1) + " " + unit
	case "s":
		return num(v, 2) + " s"
	case "ms/journey", "calls/journey":
		return num(v, 2) + " " + unit
	case "B/journey":
		return num(v, 0) + " " + unit
	}
	return num(v, 0)
}

// notReachedReason names the passes that recorded no requests for a step.
func notReachedReason(c1, cc, c2 int) string {
	var empty []string
	for _, p := range []struct {
		label string
		n     int
	}{{"B1", c1}, {"C", cc}, {"B2", c2}} {
		if p.n == 0 {
			empty = append(empty, p.label)
		}
	}
	pass := "pass "
	if len(empty) > 1 {
		pass = "passes "
	}
	return "step not reached: no requests recorded in " + pass + strings.Join(empty, ", ") +
		" (an earlier step in the journey failed), so latencies are not compared"
}

// noteUnequalCounts adds the request counts to latency findings whose passes
// recorded different numbers of requests. The comparison still stands.
func noteUnequalCounts(fs []model.Finding, counts map[key][3]int) {
	for i := range fs {
		f := &fs[i]
		if !isLatency(*f) || f.Class == model.ClassNotReached {
			continue
		}
		c, ok := counts[key{f.Journey, f.Step}]
		if !ok || (c[0] == c[1] && c[1] == c[2]) {
			continue
		}
		f.Reason += "; counts B1 " + strconv.Itoa(c[0]) + ", C " + strconv.Itoa(c[1]) + ", B2 " + strconv.Itoa(c[2])
	}
}
