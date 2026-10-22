// Package report renders run.json (and optionally scan.json) as the markdown
// note for the merge request. Every figure is formatted from a field of the
// input; nothing is computed beyond picking the worse baseline of B1 and B2.
package report

import (
	"sort"
	"strconv"
	"strings"

	"downstream/engine/issues"
	"downstream/engine/model"
)

const maxChartSteps = 20

// Render returns the report note. scan may be nil.
func Render(run model.Run, scan *model.Scan) string {
	r := renderer{run: run, scan: scan}
	r.order()
	r.header()
	r.runError()
	r.summary()
	r.journeyTables()
	r.appTable()
	r.chart()
	r.issuesToOpen()
	r.discarded()
	r.blastRadius()
	r.notMeasured()
	r.method()
	return strings.TrimRight(r.b.String(), "\n") + "\n"
}

type renderer struct {
	run  model.Run
	scan *model.Scan
	b    strings.Builder

	goals     map[string]string   // journey ID -> goal
	journeys  []string            // journey IDs that have findings, in display order
	number    map[string]int      // journey ID -> n in the chart label j<n>
	steps     map[string][]string // journey ID -> step names in display order
	byID      map[string]model.Finding
	completed bool // B1, C and B2 are all present
}

func (r *renderer) line(s string) { r.b.WriteString(s + "\n") }
func (r *renderer) blank()        { r.b.WriteString("\n") }

// order fixes the display order of journeys and steps: the order in run.Journeys
// first, then anything only the findings mention, sorted.
func (r *renderer) order() {
	r.goals = map[string]string{}
	r.number = map[string]int{}
	r.steps = map[string][]string{}
	r.byID = map[string]model.Finding{}

	stepSeen := map[string]map[string]bool{}
	addStep := func(j, s string) {
		if stepSeen[j] == nil {
			stepSeen[j] = map[string]bool{}
		}
		if !stepSeen[j][s] {
			stepSeen[j][s] = true
			r.steps[j] = append(r.steps[j], s)
		}
	}

	var ids []string
	n := 0
	for _, j := range r.run.Journeys {
		if _, dup := r.number[j.ID]; dup {
			continue
		}
		n++
		r.number[j.ID] = n
		r.goals[j.ID] = oneLine(j.Goal)
		ids = append(ids, j.ID)
		for _, s := range j.Steps {
			addStep(j.ID, s.Name)
		}
	}

	var extra []string
	has := map[string]bool{}
	var unseenSteps = map[string][]string{}
	for _, f := range r.run.Findings {
		r.byID[f.ID] = f
		if f.Journey == "" {
			continue
		}
		has[f.Journey] = true
		if _, ok := r.number[f.Journey]; !ok {
			r.number[f.Journey] = 0
			extra = append(extra, f.Journey)
		}
		if !stepSeen[f.Journey][f.Step] {
			unseenSteps[f.Journey] = append(unseenSteps[f.Journey], f.Step)
		}
	}
	sort.Strings(extra)
	for _, j := range extra {
		n++
		r.number[j] = n
		ids = append(ids, j)
	}
	for j, ss := range unseenSteps {
		sort.Strings(ss)
		for _, s := range ss {
			addStep(j, s)
		}
	}
	for _, id := range ids {
		if has[id] {
			r.journeys = append(r.journeys, id)
		}
	}

	labels := map[string]bool{}
	for _, p := range r.run.Passes {
		labels[p.Label] = true
	}
	r.completed = labels["B1"] && labels["C"] && labels["B2"]
}

func (r *renderer) goal(id string) string {
	if g := r.goals[id]; g != "" {
		return g
	}
	return id
}

func (r *renderer) header() {
	r.line("### Downstream report")
	r.blank()
	k6 := "k6 version not recorded"
	if v := r.run.Settings.K6Version; v != "" {
		k6 = oneLine(v)
	}
	r.line("Candidate `" + short(r.run.Head) + "` against baseline `" + short(r.run.Base) + "`. " +
		"Run started " + oneLine(r.run.StartedAt) + ", ended " + oneLine(r.run.EndedAt) + ". " +
		strconv.Itoa(r.run.Settings.VUs) + " VUs, " +
		strconv.Itoa(r.run.Settings.Iterations) + " iterations per VU per pass (cap " +
		strconv.Itoa(r.run.Settings.DurationSeconds) + " s), " + k6 + ".")
	r.blank()
}

func (r *renderer) runError() {
	e := r.run.Error
	if e == nil {
		return
	}
	r.line("**Run stopped at " + oneLine(e.Stage) + "**")
	r.blank()
	fence := "```"
	for strings.Contains(e.Message, fence) {
		fence += "`"
	}
	r.line(fence)
	r.line(strings.TrimRight(e.Message, "\n"))
	r.line(fence)
	r.blank()
	if !r.completed {
		r.line("No comparison was made.")
		r.blank()
	}
}

func (r *renderer) summary() {
	if !r.completed {
		return
	}
	regSteps := map[string]bool{}
	tradeJourneys := map[string]bool{}
	notReached := map[string]bool{}
	improved, withheld, appRegs := 0, 0, 0
	for _, f := range r.run.Findings {
		switch f.Class {
		case model.ClassNotReached:
			notReached[f.Journey+"\x00"+f.Step] = true
		case model.ClassRegression:
			if f.Journey == "" {
				appRegs++
			} else {
				regSteps[f.Journey+"\x00"+f.Step] = true
			}
		case model.ClassTradeOff:
			tradeJourneys[f.Journey] = true
		case model.ClassImproved:
			improved++
		case model.ClassWithheld:
			withheld++
		}
	}
	s := "**Findings:** regressed steps " + strconv.Itoa(len(regSteps)) +
		", trade-off journeys " + strconv.Itoa(len(tradeJourneys)) +
		", improved metrics " + strconv.Itoa(improved) +
		", withheld metrics " + strconv.Itoa(withheld) + "."
	if len(notReached) > 0 {
		s += " Steps not reached in at least one pass: " + strconv.Itoa(len(notReached)) + "."
	}
	if appRegs > 0 {
		s += " App process regressions: " + strconv.Itoa(appRegs) + "."
	}
	r.line(s)
	r.blank()
}

func (r *renderer) issuesToOpen() {
	if !r.completed {
		return
	}
	r.line("**Issues to open**")
	r.blank()
	plan := issues.PlanIssues(r.run, r.scan)
	if len(plan.Groups) == 0 {
		r.line("none")
	}
	for _, g := range plan.Groups {
		suffix := ""
		if n := len(g.Steps); n > 1 {
			suffix = " (" + strconv.Itoa(n) + " steps)"
		}
		r.line("- " + code(g.Headline.ID) + suffix)
	}
	if plan.RegressedSteps > 0 {
		r.blank()
		r.line(strconv.Itoa(plan.RegressedSteps) + " regressed steps are grouped into " + strconv.Itoa(plan.TotalGroups) +
			" by the changed functions on the routes they call; one issue per group.")
		if plan.LeftOutGroups > 0 {
			r.line("The " + strconv.Itoa(len(plan.Groups)) + " largest groups are listed. " + strconv.Itoa(plan.LeftOutGroups) +
				" other groups (" + strconv.Itoa(plan.LeftOutSteps) + " steps) are left out; their steps are in the tables above.")
		}
	}
	r.blank()
}

func (r *renderer) journeyTables() {
	for _, jid := range r.journeys {
		r.line("#### " + r.goal(jid))
		r.blank()
		r.line("| Step | Baseline p95 (B1 / B2) | Candidate p95 | Change | Noise band | Verdict |")
		r.line("|---|---|---|---|---|---|")
		for _, s := range r.steps[jid] {
			base := jid + "." + s + "."
			p95, hasP95 := r.byID[base+model.MetricP95]
			if hasP95 {
				r.line(row(code(s), p95))
			}
			// p50 and mean get a row only when they say something the p95 row does not.
			for _, m := range []string{model.MetricP50, model.MetricMean, model.MetricFailures} {
				f, ok := r.byID[base+m]
				if !ok || f.Class == model.ClassNoise {
					continue
				}
				if hasP95 && m != model.MetricFailures && f.Class == p95.Class {
					continue
				}
				r.line(row(code(s)+" ("+metricShort(m)+")", f))
			}
		}
		r.blank()
	}
}

func row(label string, f model.Finding) string {
	return "| " + cell(label) +
		" | " + val(f.B1, f.Unit) + " / " + val(f.B2, f.Unit) + unitSuffix(f.Unit) +
		" | " + val(f.C, f.Unit) + unitSuffix(f.Unit) +
		" | " + change(f) +
		" | " + val(f.Band, f.Unit) + unitSuffix(f.Unit) +
		" | " + cell(f.Class) + " |"
}

func (r *renderer) appTable() {
	if len(r.run.Passes) == 0 {
		return
	}
	pass := map[string]model.Pass{}
	for _, p := range r.run.Passes {
		pass[p.Label] = p
	}
	r.line("**App process**")
	r.blank()
	r.line("| Metric | B1 | C | B2 | Change | Noise band | Verdict |")
	r.line("|---|---|---|---|---|---|---|")
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
	for _, m := range []struct {
		name string
		id   string
		unit string
		get  func(model.Pass) float64
	}{
		{"CPU time", "app." + model.MetricCPU, "s", func(p model.Pass) float64 { return p.Process.CPUSeconds }},
		{"Peak RSS", "app." + model.MetricRSS, "MB", func(p model.Pass) float64 { return p.Process.PeakRSSMB }},
	} {
		chg, band, verdict := "n/a", "n/a", "not judged"
		if f, ok := r.byID[m.id]; ok {
			chg, band, verdict = change(f), val(f.Band, f.Unit)+unitSuffix(f.Unit), cell(f.Class)
		}
		r.line("| " + m.name + cells(m.get, m.unit) + " | " + chg + " | " + band + " | " + verdict + " |")
	}
	r.blank()
}

type point struct {
	label string
	jn    int
	f     model.Finding
}

func (r *renderer) chart() {
	var pts []point
	for _, jid := range r.journeys {
		for _, s := range r.steps[jid] {
			if f, ok := r.byID[jid+"."+s+"."+model.MetricP95]; ok && f.Class != model.ClassNotReached {
				pts = append(pts, point{label: chartLabel("j" + strconv.Itoa(r.number[jid]) + "." + s), jn: r.number[jid], f: f})
			}
		}
	}
	if len(pts) == 0 {
		return
	}
	total := len(pts)
	if total > maxChartSteps {
		keep := make([]int, total)
		for i := range keep {
			keep[i] = i
		}
		sort.SliceStable(keep, func(a, b int) bool { return pts[keep[a]].f.Delta > pts[keep[b]].f.Delta })
		keep = keep[:maxChartSteps]
		sort.Ints(keep)
		sel := make([]point, 0, maxChartSteps)
		for _, i := range keep {
			sel = append(sel, pts[i])
		}
		pts = sel
	}

	labels := make([]string, len(pts))
	bars := make([]string, len(pts))
	line := make([]string, len(pts))
	charted := map[int]bool{}
	for i, p := range pts {
		labels[i] = `"` + p.label + `"`
		bars[i] = val(max(p.f.B1, p.f.B2), "ms")
		line[i] = val(p.f.C, "ms")
		charted[p.jn] = true
	}

	r.line("**p95 per step**")
	r.blank()
	r.line("```mermaid")
	r.line("xychart-beta")
	r.line(`    title "p95 per step: candidate against the worse baseline run"`)
	r.line("    x-axis [" + strings.Join(labels, ", ") + "]")
	r.line(`    y-axis "p95 (ms)"`)
	r.line("    bar [" + strings.Join(bars, ", ") + "]")
	r.line("    line [" + strings.Join(line, ", ") + "]")
	r.line("```")
	r.blank()
	r.line("Bars: worse of the two baseline runs. Line: candidate.")
	if total > maxChartSteps {
		r.line("The chart shows the " + strconv.Itoa(maxChartSteps) + " steps with the largest change, out of " +
			strconv.Itoa(total) + ".")
	}
	r.blank()
	for _, jid := range r.journeys {
		if charted[r.number[jid]] {
			r.line("- j" + strconv.Itoa(r.number[jid]) + ": " + r.goal(jid))
		}
	}
	r.blank()
}

func (r *renderer) discarded() {
	r.line("**Discarded journeys**")
	r.blank()
	if len(r.run.Discarded) == 0 {
		r.line("none")
	}
	for _, d := range r.run.Discarded {
		r.line("- " + code(d.Journey) + ", step " + code(d.Step) + ": " + oneLine(d.Reason))
	}
	r.blank()
}

func (r *renderer) blastRadius() {
	if r.scan == nil {
		return
	}
	r.line("**Blast radius**")
	r.blank()
	r.line("Changed routes:")
	r.blank()
	r.routes(r.scan.Changed)
	r.blank()
	r.line("Affected routes:")
	r.blank()
	r.routes(r.scan.Affected)
	r.blank()
	if m := oneLine(r.scan.Method); m != "" {
		r.line("*" + m + "*")
		r.blank()
	}
}

func (r *renderer) routes(impacts []model.Impact) {
	if len(impacts) == 0 {
		r.line("- none")
		return
	}
	for _, im := range impacts {
		s := "- " + code(strings.TrimSpace(im.Method+" "+im.Path))
		if len(im.Reasons) > 0 {
			s += ": " + oneLine(im.Reasons[0])
		}
		r.line(s)
	}
}

func (r *renderer) notMeasured() {
	r.line("**Not measured**")
	r.blank()
	if len(r.run.NotMeasured) == 0 {
		r.line("- nothing listed")
	}
	for _, s := range r.run.NotMeasured {
		r.line("- " + oneLine(s))
	}
	r.blank()
}

func (r *renderer) method() {
	r.line("**Method.** Baseline run twice (B1, B2) and candidate once (C), in the order B1, C, B2, " +
		"sequentially on one machine. Three runs give no confidence interval; treat verdicts near the thresholds as uncertain.")
	r.blank()
	t := r.run.Settings.Thresholds
	if t == (model.Thresholds{}) {
		r.line("Thresholds were not recorded in this run.")
		return
	}
	r.line("Thresholds in effect: a metric is a regression when the candidate is worse than the worse baseline run by more than " +
		fixed(t.BandMultiplier) + " times the noise band (the difference between B1 and B2), by at least " +
		fixed(t.MinRelative*100) + "%, and by at least " + fixed(t.MinLatencyMS) + " ms (latency), " +
		fixed(t.MinMemoryMB) + " MB (memory) or " + fixed(t.MinCPUSeconds) + " s (CPU). A step that passed on both baseline runs and failed on the candidate is a regression. " +
		"The verdict is withheld when the baseline runs differ by more than " + fixed(t.MaxBaselineSpread*100) +
		"% of the smaller one and by at least the same minimum, unless the candidate is clearly worse or better than both. A journey with a latency regression and a latency improvement is reported as a trade-off.")
}

// Formatting helpers.

// val formats a figure with fixed decimals: ms 1, s 2, MB 1, count 0.
func val(v float64, unit string) string {
	switch unit {
	case "s":
		return strconv.FormatFloat(v, 'f', 2, 64)
	case "count":
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

func unitSuffix(unit string) string {
	if unit == "count" {
		return ""
	}
	return " " + unit
}

// signed formats with an explicit sign and never prints "-0.0".
func signed(v float64, dec int) string {
	s := strconv.FormatFloat(v, 'f', dec, 64)
	if strings.Trim(s, "-0.") == "" {
		return "+" + strings.TrimPrefix(s, "-")
	}
	if !strings.HasPrefix(s, "-") {
		return "+" + s
	}
	return s
}

// change is the Change column: the delta against the worse baseline, with a
// percentage when the worse baseline is not zero (otherwise it is undefined).
func change(f model.Finding) string {
	if f.Class == model.ClassNotReached {
		return "n/a"
	}
	dec := 1
	switch f.Unit {
	case "s":
		dec = 2
	case "count":
		dec = 0
	}
	s := signed(f.Delta, dec) + unitSuffix(f.Unit)
	if max(f.B1, f.B2) != 0 {
		s += " (" + signed(f.DeltaPct, 0) + "%)"
	}
	return s
}

// fixed prints a threshold without trailing zeros.
func fixed(v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0")
}

func metricShort(m string) string {
	return strings.TrimSuffix(m, "_ms")
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// oneLine keeps free text inside one markdown line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// code puts text in a code span, dropping characters that would end it.
func code(s string) string {
	s = strings.ReplaceAll(oneLine(s), "`", "'")
	return "`" + s + "`"
}

// cell makes text safe for a table cell.
func cell(s string) string {
	return strings.ReplaceAll(oneLine(s), "|", `\|`)
}

func chartLabel(s string) string {
	s = strings.NewReplacer(`"`, "", "'", "", "`", "", "[", "", "]", "", ",", "").Replace(oneLine(s))
	return s
}
