package report

import (
	"strconv"

	"downstream/engine/journey"
	"downstream/engine/model"
)

// newFeature writes the new journeys' figures from pass C. Those journeys use
// routes the base does not have, so there is nothing to compare: figures are
// shown as measured and only failed requests count against the change.
func (r *renderer) newFeature() {
	nf := r.run.NewFeature
	if nf == nil {
		return
	}
	r.line("#### New feature (new build only)")
	r.blank()
	r.line("These journeys use routes this merge request adds, so they ran on the new build only, in pass C alongside the journeys above, and have no baseline. Figures are as measured; failed requests are the only verdict. Because they ran in C, the old-against-new figures above show existing pages while the new feature is in use, and C's app CPU time includes their work.")
	r.blank()
	r.line("| Journey | Step | Requests | Failed | p50 | p95 | Verdict |")
	r.line("|---|---|---|---|---|---|---|")
	got := map[string]model.StepMetrics{}
	for _, s := range nf.Pass.Steps {
		got[s.Journey+"."+s.Step] = s
	}
	for _, j := range nf.Journeys {
		for _, st := range j.Steps {
			s, ok := got[j.ID+"."+st.Name]
			if !ok || s.Count == 0 {
				r.line("| " + code(j.ID) + " | " + code(st.Name) + " | 0 | 0 | | | not reached |")
				continue
			}
			verdict := "ok"
			if s.Failures > 0 {
				verdict = "failing"
			}
			r.line("| " + code(j.ID) + " | " + code(st.Name) + " | " + strconv.Itoa(s.Count) + " | " + strconv.Itoa(s.Failures) +
				" | " + val(s.P50, "ms") + unitSuffix("ms") + " | " + val(s.P95, "ms") + unitSuffix("ms") + " | " + verdict + " |")
		}
	}
	r.blank()
}

// NewFailures counts new-feature steps with failed requests.
func NewFailures(run model.Run) int {
	if run.NewFeature == nil {
		return 0
	}
	n := 0
	for _, s := range run.NewFeature.Pass.Steps {
		if s.Failures > 0 {
			n++
		}
	}
	return n
}

// coverage lists new routes and any route in the change that no journey that
// actually ran reached.
func (r *renderer) coverage() {
	if len(r.scan.NewRoutes) > 0 {
		r.line("New routes (only in the new build):")
		r.blank()
		for _, rt := range r.scan.NewRoutes {
			r.line("- " + code(routeLabel(rt)))
		}
		r.blank()
	}
	ran := model.JourneyFile{Version: model.FormatVersion, Journeys: append([]model.Journey{}, r.run.Journeys...)}
	if r.run.NewFeature != nil {
		ran.Journeys = append(ran.Journeys, r.run.NewFeature.Journeys...)
	}
	uncovered, _ := journey.Coverage(ran, *r.scan)
	if len(uncovered) == 0 {
		r.line("Coverage: every changed, affected and new route had a journey step that ran.")
		r.blank()
		return
	}
	r.line("Coverage: these routes in the change had no journey step that ran, so they were not measured:")
	r.blank()
	for _, rt := range uncovered {
		r.line("- " + code(routeLabel(rt)))
	}
	r.blank()
}

func routeLabel(rt model.Route) string {
	if rt.Method == "" {
		return rt.Path
	}
	return rt.Method + " " + rt.Path
}
