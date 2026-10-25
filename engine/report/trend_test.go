package report

import (
	"strings"
	"testing"

	"downstream/engine/history"
)

func TestRenderTrend(t *testing.T) {
	out := RenderTrend(history.Trend{Status: "ok", Window: 10, Limit: 0.25, Service: "https://memory.example",
		Lines: []history.Line{
			{Label: "CPU per completed journey", Merged: 2, MRs: []int{4, 7}, Before: 0.188, This: 0.09, IfMerged: 0.2949, Creeping: true},
			{Label: "Peak RSS", Merged: 0, MRs: []int{}, This: 0.01, IfMerged: 0.01},
		}})
	for _, want := range []string{
		"#### Trend across merged merge requests",
		"**Trend:** 1 of 2 metrics would have risen by 25% or more",
		"| CPU per completed journey | 2 (!4, !7) | +18.8% | +9.0% | +29.5% | **yes** |",
		"| Peak RSS | none | +0.0% | +1.0% | +1.0% | no |",
		"https://memory.example",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	un := RenderTrend(history.Trend{Status: "unavailable", Reason: "the memory service did not answer"})
	if !strings.Contains(un, "Trend: not shown, the memory service did not answer.") {
		t.Errorf("unavailable:\n%s", un)
	}
}
