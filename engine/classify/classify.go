// Package classify turns the three passes (B1, C, B2) into findings.
//
// Every metric is "higher is worse". The noise band is the difference between
// the two baseline runs; the candidate is judged against the worse baseline.
package classify

import (
	"math"
	"sort"
	"strconv"

	"downstream/engine/model"
)

// DefaultThresholds are the initial rules from agentnotes.md.
func DefaultThresholds() model.Thresholds {
	return model.Thresholds{
		BandMultiplier:    2,
		MinRelative:       0.20,
		MinLatencyMS:      10,
		MinMemoryMB:       10,
		MaxBaselineSpread: 0.5,
	}
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
	for k, mc := range idxC {
		m1, ok1 := idx1[k]
		m2, ok2 := idx2[k]
		if !ok1 || !ok2 {
			continue
		}
		id := k.journey + "." + k.step + "."
		add := func(metric, unit string, v1, vc, v2 float64) {
			f := judge(v1, vc, v2, unit, t)
			f.ID = id + metric
			f.Journey, f.Step, f.Metric, f.Unit = k.journey, k.step, metric, unit
			out = append(out, f)
		}
		add(model.MetricP95, "ms", m1.P95, mc.P95, m2.P95)
		add(model.MetricP50, "ms", m1.P50, mc.P50, m2.P50)
		add(model.MetricMean, "ms", m1.Mean, mc.Mean, m2.Mean)
		add(model.MetricFailures, "count", float64(m1.Failures), float64(mc.Failures), float64(m2.Failures))
	}

	app := func(metric, unit string, v1, vc, v2 float64) {
		f := judge(v1, vc, v2, unit, t)
		f.ID = "app." + metric
		f.Metric, f.Unit = metric, unit
		out = append(out, f)
	}
	app(model.MetricCPU, "s", b1.Process.CPUSeconds, c.Process.CPUSeconds, b2.Process.CPUSeconds)
	app(model.MetricRSS, "MB", b1.Process.PeakRSSMB, c.Process.PeakRSSMB, b2.Process.PeakRSSMB)

	tradeOffs(out)
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

// judge fills every Finding field except ID, Journey, Step, Metric and Unit.
func judge(v1, vc, v2 float64, unit string, t model.Thresholds) model.Finding {
	band := math.Abs(v1 - v2)
	worse, better := math.Max(v1, v2), math.Min(v1, v2)
	f := model.Finding{B1: v1, C: vc, B2: v2, Band: band, Delta: vc - worse}
	if worse != 0 {
		f.DeltaPct = 100 * f.Delta / worse
	}

	if unit == "count" {
		f.Class, f.Reason = judgeFailures(v1, vc, v2)
		return f
	}

	floor, floorWords := 0.0, ""
	switch unit {
	case "ms":
		floor = t.MinLatencyMS
	case "MB":
		floor = t.MinMemoryMB
	}
	if floor > 0 {
		floorWords = " and at least " + amount(floor, unit)
	}
	// A spread below the absolute floor is jitter on a fast step, not a noisy
	// baseline, so it does not stop a verdict.
	if better > 0 && band/better > t.MaxBaselineSpread && band >= floor {
		f.Class = model.ClassWithheld
		f.Reason = "baseline spread rule: the two baseline runs differ by " +
			num(band/better, 2) + " of the smaller one, above the limit of " +
			num(t.MaxBaselineSpread, -1) + ", so no verdict is given"
		return f
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

// tradeOffs re-classes latency findings on a journey that has both a clear
// regression and a clear improvement. Failures stay what they are.
func tradeOffs(fs []model.Finding) {
	type seen struct{ worse, better bool }
	byJourney := map[string]*seen{}
	for _, f := range fs {
		if f.Journey == "" || !isLatency(f.Metric) {
			continue
		}
		s := byJourney[f.Journey]
		if s == nil {
			s = &seen{}
			byJourney[f.Journey] = s
		}
		switch f.Class {
		case model.ClassRegression:
			s.worse = true
		case model.ClassImproved:
			s.better = true
		}
	}
	for i := range fs {
		f := &fs[i]
		s := byJourney[f.Journey]
		if s == nil || !s.worse || !s.better || !isLatency(f.Metric) {
			continue
		}
		if f.Class == model.ClassRegression || f.Class == model.ClassImproved {
			f.Class = model.ClassTradeOff
			f.Reason = "one metric clearly better and another clearly worse on the same journey"
		}
	}
}

func isLatency(metric string) bool {
	return metric == model.MetricP95 || metric == model.MetricP50 || metric == model.MetricMean
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
	}
	return num(v, 0)
}
