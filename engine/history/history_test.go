package history

import (
	"math"
	"strings"
	"testing"

	"downstream/engine/model"
)

func sha(c string) string { return strings.Repeat(c, 40) }

func entry(mr int, head, recorded string, cpu float64) Entry {
	return Entry{Version: 1, Project: "g/p", MR: mr, Base: sha("0"), Head: head, Recorded: recorded,
		Metrics: map[string]Metric{"app.cpu_ms_per_journey": {Label: "CPU per completed journey", Unit: "ms/journey", Base: 10, C: 10 * (1 + cpu), Change: cpu}}}
}

func TestSummarizeKeepsComparedGatedAppMetrics(t *testing.T) {
	run := model.Run{Base: sha("a"), Head: sha("b"), Findings: []model.Finding{
		{ID: "app.cpu_ms_per_journey", Metric: "CPU per completed journey", Unit: "ms/journey", B1: 9, B2: 10, C: 12, Gated: true, Class: model.ClassNoise},
		{ID: "app.peak_rss_mb", Metric: "Peak RSS", Unit: "MB", B1: 5, B2: 5, C: 5, Gated: true, Class: model.ClassWithheld},
		{ID: "j.s.p95", Journey: "j", Step: "s", Metric: "p95", B1: 1, B2: 1, C: 9, Class: model.ClassRegression},
		{ID: "app.cpu_seconds", Metric: "CPU time", B1: 1, B2: 1, C: 2, Class: model.ClassRegression},
	}}
	e, err := Summarize(run, "g/p", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Metrics) != 1 {
		t.Fatalf("metrics = %v", e.Metrics)
	}
	m := e.Metrics["app.cpu_ms_per_journey"]
	if m.Base != 10 || math.Abs(m.Change-0.2) > 1e-9 {
		t.Fatalf("cpu = %+v (want the worse baseline 10 and +20%%)", m)
	}
}

func TestComputeCompoundsOnlyMergedRecords(t *testing.T) {
	prior := []Entry{
		entry(1, sha("1"), "2026-10-20T10:00:00Z", 0.08),
		entry(2, sha("2"), "2026-10-21T10:00:00Z", 0.08),
		entry(2, sha("3"), "2026-10-21T11:00:00Z", 0.10), // newer run of MR 2: the one merged
		entry(4, sha("4"), "2026-10-22T10:00:00Z", 0.50), // never merged
	}
	merged := map[string]bool{sha("1"): true, sha("2"): true, sha("3"): true}
	cur := entry(5, sha("5"), "", 0.09)
	tr := Compute(cur, prior, func(h string) bool { return merged[h] })
	if len(tr.Lines) != 1 {
		t.Fatalf("lines = %+v", tr.Lines)
	}
	l := tr.Lines[0]
	want := 1.08*1.10*1.09 - 1
	if l.Merged != 2 || math.Abs(l.IfMerged-want) > 1e-9 || !l.Creeping {
		t.Fatalf("line = %+v, want 2 merged MRs, %.4f compounded, creeping", l, want)
	}
	if l.MRs[0] != 1 || l.MRs[1] != 2 {
		t.Fatalf("mrs = %v, want [1 2] oldest first", l.MRs)
	}
}

func TestComputeWithNoHistoryIsNotCreeping(t *testing.T) {
	tr := Compute(entry(5, sha("5"), "", 0.3), nil, func(string) bool { return true })
	if l := tr.Lines[0]; l.Merged != 0 || l.Creeping {
		t.Fatalf("line = %+v", l)
	}
}

func TestValidateRejectsBadRecords(t *testing.T) {
	bad := entry(1, "nothex", "", 0.1)
	if err := bad.Validate(); err == nil {
		t.Fatal("bad SHA accepted")
	}
	inf := entry(1, sha("1"), "", math.Inf(1))
	if err := inf.Validate(); err == nil {
		t.Fatal("infinite change accepted")
	}
}
