package report

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"downstream/engine/model"
)

// The sections below render a run.json whose findings carry a Kind. Gated
// metrics decide; latency is shown for context and is labelled advisory.

// gatedOrder is the display order of the app-wide gated metrics. Any other
// gated app finding follows, by ID.
var gatedOrder = []string{
	model.MetricCPUPerJourney, model.MetricWriteBytesPerJourney, model.MetricWriteSyscallsPerJourney,
	model.MetricOpenFDs, model.MetricThreads, model.MetricRSS,
	model.MetricAllocObjectsPerJourney, model.MetricAllocBytesPerJourney, model.MetricGCCyclesPerJourney, model.MetricGoroutines,
}

var gatedNames = map[string]string{
	model.MetricCPUPerJourney:           "CPU per completed journey",
	model.MetricWriteBytesPerJourney:    "Bytes written per journey",
	model.MetricWriteSyscallsPerJourney: "Write syscalls per journey",
	model.MetricOpenFDs:                 "Open fds",
	model.MetricThreads:                 "Threads",
	model.MetricRSS:                     "Peak RSS",
	model.MetricAllocObjectsPerJourney:  "Heap objects allocated per journey",
	model.MetricAllocBytesPerJourney:    "Heap bytes allocated per journey",
	model.MetricGCCyclesPerJourney:      "GC cycles per journey",
	model.MetricGoroutines:              "Goroutines",
}

func (r *renderer) gatedSummary() {
	if !r.completed {
		return
	}
	regSteps := map[string]bool{}
	notReached := map[string]bool{}
	var appRegs, tradeOffs, improved, withheld, notMeasured, slower, faster int
	for _, f := range r.run.Findings {
		if f.Class == model.ClassNotReached {
			notReached[f.Journey+"\x00"+f.Step] = true
		}
		if !f.Decides() {
			switch f.Class {
			case model.ClassRegression:
				slower++
			case model.ClassImproved:
				faster++
			}
			continue
		}
		switch f.Class {
		case model.ClassRegression:
			if f.Journey == "" {
				appRegs++
			} else {
				regSteps[f.Journey+"\x00"+f.Step] = true
			}
		case model.ClassTradeOff:
			tradeOffs++
		case model.ClassImproved:
			improved++
		case model.ClassWithheld:
			withheld++
		case model.ClassNotMeasured:
			notMeasured++
		}
	}
	s := "**Findings (gated):** regressed app metrics " + strconv.Itoa(appRegs) +
		", failing steps " + strconv.Itoa(len(regSteps)) +
		", trade-off metrics " + strconv.Itoa(tradeOffs) +
		", improved metrics " + strconv.Itoa(improved) +
		", withheld metrics " + strconv.Itoa(withheld) +
		", not measured " + strconv.Itoa(notMeasured) + "."
	if len(notReached) > 0 {
		s += " Steps not reached in at least one pass: " + strconv.Itoa(len(notReached)) + "."
	}
	r.line(s)
	r.blank()
	r.line("**Latency (advisory):** slower " + strconv.Itoa(slower) + ", faster " + strconv.Itoa(faster) +
		". Latency does not decide the verdict.")
	r.blank()
}

// gatedFindings returns the app-wide gated findings in display order.
func (r *renderer) gatedFindings() []model.Finding {
	var out []model.Finding
	seen := map[string]bool{}
	for _, m := range gatedOrder {
		if f, ok := r.byID["app."+m]; ok && f.Decides() {
			out = append(out, f)
			seen["app."+m] = true
		}
	}
	var rest []model.Finding
	for _, f := range r.run.Findings {
		if f.Journey == "" && f.Gated && !seen[f.ID] {
			rest = append(rest, f)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].ID < rest[j].ID })
	return append(out, rest...)
}

func (r *renderer) gatedTable() {
	if !r.completed {
		return
	}
	r.line("**Gated findings (counters, CPU per journey, memory)**")
	r.blank()
	fs := r.gatedFindings()
	if len(fs) == 0 {
		r.line("No gated metric was judged in this run.")
		r.blank()
		return
	}
	r.line("| Metric | B1 | B2 | C | Change | Noise band | Allowed | Smallest detectable | Verdict |")
	r.line("|---|---|---|---|---|---|---|---|---|")
	for _, f := range fs {
		name := gatedNames[f.Metric]
		if name == "" {
			name = f.Metric
		}
		if f.Class == model.ClassNotMeasured {
			r.line("| " + cell(name) + " | not measured | not measured | not measured | - | - | - | - | " + cell(f.Class) + " |")
			continue
		}
		r.line("| " + cell(name) +
			" | " + val(f.B1, f.Unit) + unitSuffix(f.Unit) +
			" | " + val(f.B2, f.Unit) + unitSuffix(f.Unit) +
			" | " + val(f.C, f.Unit) + unitSuffix(f.Unit) +
			" | " + change(f) +
			" | " + val(f.Band, f.Unit) + unitSuffix(f.Unit) +
			" | " + val(f.Allowed, f.Unit) + unitSuffix(f.Unit) +
			" | " + detectable(f) +
			" | " + cell(f.Class) + " |")
	}
	r.blank()
	r.line("Change is against the worse baseline run. A rise counts only when it is more than both the allowed rise and the noise band. " +
		"Smallest detectable is the larger of the two: the smallest change this run could tell apart from noise.")
	r.blank()
	for _, f := range fs {
		if f.Class == model.ClassNoise {
			continue
		}
		r.line("- " + code(f.ID) + ": " + oneLine(f.Reason))
	}
	r.blank()
}

// detectable is the smallest-detectable cell: the figure, and its share of the
// worse baseline when that is defined.
func detectable(f model.Finding) string {
	if f.Detectable == 0 {
		return "n/a"
	}
	s := val(f.Detectable, f.Unit) + unitSuffix(f.Unit)
	if f.DetectablePct != 0 {
		s += " (" + strconv.FormatFloat(f.DetectablePct, 'f', 1, 64) + "%)"
	}
	return s
}

func (r *renderer) failuresSection() {
	if !r.completed {
		return
	}
	r.line("**Step failures**")
	r.blank()
	var rows []model.Finding
	for _, jid := range r.journeys {
		for _, s := range r.steps[jid] {
			f, ok := r.byID[jid+"."+s+"."+model.MetricFailures]
			if !ok {
				continue
			}
			if f.Class != model.ClassNoise || f.B1 > 0 || f.C > 0 || f.B2 > 0 {
				rows = append(rows, f)
			}
		}
	}
	if len(rows) == 0 {
		r.line("No step recorded a failure in any pass.")
		r.blank()
		return
	}
	r.line("| Journey | Step | Failures B1 / B2 | Failures C | Change | Noise band | Smallest detectable | Verdict |")
	r.line("|---|---|---|---|---|---|---|---|")
	for _, f := range rows {
		r.line("| " + cell(r.goal(f.Journey)) + " | " + code(f.Step) +
			" | " + val(f.B1, f.Unit) + " / " + val(f.B2, f.Unit) +
			" | " + val(f.C, f.Unit) +
			" | " + change(f) +
			" | " + val(f.Band, f.Unit) +
			" | " + detectable(f) +
			" | " + cell(f.Class) + " |")
	}
	r.blank()
	r.line("A step that passed on both baseline runs and failed on the candidate is a regression. One failure is enough to see.")
	r.blank()
}

// advisoryWord names an advisory verdict without the language of a decision.
func advisoryWord(class string) string {
	switch class {
	case model.ClassRegression:
		return "slower"
	case model.ClassImproved:
		return "faster"
	case model.ClassNoise:
		return "noise"
	case model.ClassWithheld:
		return "baselines disagree"
	case model.ClassNotReached:
		return "not reached"
	}
	return class
}

func advisoryRow(label string, f model.Finding) string {
	return "| " + cell(label) +
		" | " + val(f.B1, f.Unit) + " / " + val(f.B2, f.Unit) + unitSuffix(f.Unit) +
		" | " + val(f.C, f.Unit) + unitSuffix(f.Unit) +
		" | " + change(f) +
		" | " + val(f.Band, f.Unit) + unitSuffix(f.Unit) +
		" | " + detectable(f) +
		" | " + advisoryWord(f.Class) + " |"
}

func (r *renderer) advisorySection() {
	if !r.completed {
		return
	}
	r.line("**Latency (advisory, not used for verdicts)**")
	r.blank()
	r.line("Latency of identical code varied far more between runs than the gated metrics, so it does not decide whether a run has a regression. " +
		"Slower and faster mean only that the latency rule's thresholds were passed. Smallest detectable is the smallest rise that rule could have called slower in this run.")
	r.blank()
	for _, jid := range r.journeys {
		r.line("#### " + r.goal(jid))
		r.blank()
		r.line("| Step | Baseline p95 (B1 / B2) | Candidate p95 | Change | Noise band | Smallest detectable | Verdict |")
		r.line("|---|---|---|---|---|---|---|")
		for _, s := range r.steps[jid] {
			base := jid + "." + s + "."
			p95, hasP95 := r.byID[base+model.MetricP95]
			if hasP95 {
				r.line(advisoryRow(code(s), p95))
			}
			// p50 and mean get a row only when they say something the p95 row does not.
			for _, m := range []string{model.MetricP50, model.MetricMean} {
				f, ok := r.byID[base+m]
				if !ok || f.Class == model.ClassNoise {
					continue
				}
				if hasP95 && f.Class == p95.Class {
					continue
				}
				r.line(advisoryRow(code(s)+" ("+metricShort(m)+")", f))
			}
		}
		r.blank()
	}
	if f, ok := r.byID["app."+model.MetricCPU]; ok {
		r.line("App total CPU time depends on how many journeys completed, so CPU per completed journey above is the gated figure.")
		r.blank()
		r.line("| Metric | B1 / B2 | C | Change | Noise band | Smallest detectable | Verdict |")
		r.line("|---|---|---|---|---|---|---|")
		r.line(advisoryRow("CPU time (total)", f))
		r.blank()
	}
}

// processTable shows the raw per-pass process figures. Verdicts are in the
// gated table above.
func (r *renderer) processTable() {
	if len(r.run.Passes) == 0 {
		return
	}
	pass := map[string]model.Pass{}
	for _, p := range r.run.Passes {
		pass[p.Label] = p
	}
	r.line("**App process, per pass**")
	r.blank()
	r.line("| Metric | B1 | C | B2 |")
	r.line("|---|---|---|---|")
	cells := func(get func(model.Pass) float64, unit string) string {
		var out string
		for _, l := range []string{"B1", "C", "B2"} {
			p, ok := pass[l]
			if !ok {
				out += " | not run"
				continue
			}
			out += " | " + val(get(p), unit) + unitSuffix(unit)
		}
		return out
	}
	r.line("| CPU time, 10 ms ticks" + cells(func(p model.Pass) float64 { return p.Process.CPUSeconds }, "s") + " |")
	r.line("| Peak RSS" + cells(func(p model.Pass) float64 { return p.Process.PeakRSSMB }, "MB") + " |")
	r.rawCounterRows(pass)
	r.blank()
}

func (r *renderer) rawCounterRows(pass map[string]model.Pass) {
	has := false
	for _, p := range pass {
		if p.Process.CPUSource != "" {
			has = true
		}
	}
	if !has {
		return
	}
	for _, m := range counterSpecs {
		line := "| " + m.name
		for _, l := range []string{"B1", "C", "B2"} {
			p, ok := pass[l]
			switch {
			case !ok:
				line += " | not run"
			case m.unmeasured != "" && slices.Contains(p.Process.Unmeasured, m.unmeasured):
				line += " | not measured"
			default:
				line += " | " + m.format(m.get(p.Process)) + m.unit
			}
		}
		r.line(line + " |")
	}
	for _, l := range []string{"B1", "C", "B2"} {
		if pass[l].Process.CPUSource == model.CPUSourceProcStat {
			r.blank()
			r.line("CPU time (exact) is the 10 ms /proc tick count in at least one pass, because the kernel gave no resource usage for the exited process.")
			break
		}
	}
}

// toleranceLine lists the gated tolerances in effect, in display order.
func toleranceLine(t model.Thresholds) string {
	var parts []string
	for _, m := range gatedOrder {
		tol, ok := t.Tolerances[m]
		if !ok {
			continue
		}
		var how []string
		if tol.Relative > 0 {
			how = append(how, fixed(tol.Relative*100)+"% of the worse baseline")
		}
		if tol.Absolute > 0 {
			how = append(how, "an absolute "+strconv.FormatFloat(tol.Absolute, 'f', -1, 64))
		}
		name := gatedNames[m]
		if name == "" {
			name = m
		}
		parts = append(parts, name+" "+strings.Join(how, ", the larger of the two, "))
	}
	return strings.Join(parts, "; ")
}
