package journey

import (
	"fmt"

	"downstream/engine/model"
)

// Lint reports gaps in how well the journeys exercise the scanned change. It
// only advises: every finding is a sentence for the reviewer, never an error.
// A changed route needs at least one step that expects a non-2xx status,
// because a journey of successes alone passes on most real regressions. An
// affected route only needs to be reached.
func Lint(f model.JourneyFile, scan model.Scan) []string {
	type coverage struct{ steps, nonSuccess int }
	cov := map[string]*coverage{}
	for _, j := range f.Journeys {
		for _, s := range j.Steps {
			r, ok := MatchRoute(s.Method, s.Path, scan.Routes)
			if !ok {
				continue
			}
			c := cov[routeKey(r)]
			if c == nil {
				c = &coverage{}
				cov[routeKey(r)] = c
			}
			c.steps++
			if s.ExpectStatus < 200 || s.ExpectStatus > 299 {
				c.nonSuccess++
			}
		}
	}

	var out []string
	seen := map[string]bool{}
	for _, im := range scan.Changed {
		k := routeKey(im.Route)
		if seen[k] {
			continue
		}
		seen[k] = true
		switch c := cov[k]; {
		case c == nil:
			out = append(out, fmt.Sprintf("changed route %s is not covered by any journey", k))
		case c.nonSuccess == 0:
			out = append(out, fmt.Sprintf("changed route %s is covered only by steps that expect a 2xx status; add a negative or edge-input step", k))
		}
	}
	for _, im := range scan.Affected {
		k := routeKey(im.Route)
		if seen[k] {
			continue
		}
		seen[k] = true
		if cov[k] == nil {
			out = append(out, fmt.Sprintf("affected route %s is not covered by any journey", k))
		}
	}
	return out
}

func routeKey(r model.Route) string {
	if r.Method == "" {
		return r.Path
	}
	return r.Method + " " + r.Path
}
