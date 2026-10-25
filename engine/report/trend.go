package report

import (
	"fmt"
	"strconv"
	"strings"

	"downstream/engine/history"
)

// RenderTrend returns the Trend section: each gated metric's change compounded
// over the merged merge requests the memory service holds, with and without
// this one. Every figure comes from trend.json.
func RenderTrend(t history.Trend) string {
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }
	line("#### Trend across merged merge requests")
	line("")
	if t.Status != "ok" {
		line("Trend: not shown, " + oneLine(t.Reason) + ". The rest of this report does not depend on it.")
		return b.String()
	}
	creeping := 0
	for _, l := range t.Lines {
		if l.Creeping {
			creeping++
		}
	}
	line(fmt.Sprintf("**Trend:** %d of %d metrics would have risen by %s or more across the last merged merge requests if this one merges.",
		creeping, len(t.Lines), pct(t.Limit)))
	line("")
	line("| Metric | Merged MRs counted | Their compounded change | This MR | Compounded if merged | Creep |")
	line("|---|---|---|---|---|---|")
	for _, l := range t.Lines {
		mrs := "none"
		if l.Merged > 0 {
			var s []string
			for _, n := range l.MRs {
				s = append(s, "!"+strconv.Itoa(n))
			}
			mrs = strconv.Itoa(l.Merged) + " (" + strings.Join(s, ", ") + ")"
		}
		creep := "no"
		if l.Creeping {
			creep = "**yes**"
		}
		line(fmt.Sprintf("| %s | %s | %s | %s | %s | %s |", cell(l.Label), mrs, signedPct(l.Before), signedPct(l.This), signedPct(l.IfMerged), creep))
	}
	line("")
	line(fmt.Sprintf("Method: each merge request's change is its candidate against the worse of its two baselines, measured in one session, so changes from different sessions can be compounded even though absolute figures differ between sessions. A merged merge request is one whose recorded head is an ancestor of this run's base (squash merges are not traced). Window: the last %d merged merge requests.", t.Window))
	if t.Service != "" {
		line("")
		line("History is kept by the Downstream memory service on Google Cloud Run: " + t.Service + " (numbers only, deleted after 30 days).")
	}
	return b.String()
}

func pct(v float64) string { return strconv.FormatFloat(v*100, 'f', 0, 64) + "%" }

func signedPct(v float64) string { return signed(v*100, 1) + "%" }
