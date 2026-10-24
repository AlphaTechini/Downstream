package journey

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"downstream/engine/model"
)

// Coverage checks that the journeys cover the change: every changed and
// affected route and every new route is hit by a step, new routes only by new
// journeys, and at least one control journey runs when the app has routes
// outside the change. When the cap is reached, routes left uncovered are
// returned instead of failing, so the report can list them.
func Coverage(f model.JourneyFile, sc model.Scan) (uncovered []model.Route, err error) {
	var errs []error
	isNew := map[string]bool{}
	for _, r := range sc.NewRoutes {
		isNew[routeKey(r)] = true
	}
	inChange := map[string]bool{}
	var need []model.Route
	for _, list := range [][]model.Impact{sc.Changed, sc.Affected} {
		for _, im := range list {
			if k := routeKey(im.Route); !inChange[k] {
				inChange[k] = true
				need = append(need, im.Route)
			}
		}
	}
	for _, r := range sc.NewRoutes {
		if k := routeKey(r); !inChange[k] {
			inChange[k] = true
			need = append(need, r)
		}
	}

	hit := map[string]bool{}
	controls := 0
	for _, j := range f.Journeys {
		if j.Kind == model.KindControl {
			controls++
		}
		for _, s := range j.Steps {
			r, ok := MatchRoute(s.Method, s.Path, sc.Routes)
			if !ok {
				continue
			}
			k := routeKey(r)
			hit[k] = true
			if isNew[k] && !j.IsNew() {
				errs = append(errs, fmt.Errorf("journey %q step %q uses %s, which only the new build has; set the journey's kind to %q", j.ID, s.Name, label(r), model.KindNew))
			}
		}
	}
	for _, r := range need {
		if !hit[routeKey(r)] {
			uncovered = append(uncovered, r)
		}
	}
	sort.Slice(uncovered, func(a, b int) bool { return label(uncovered[a]) < label(uncovered[b]) })
	if len(uncovered) > 0 && len(f.Journeys) < maxJourneys {
		names := make([]string, len(uncovered))
		for i, r := range uncovered {
			names[i] = label(r)
		}
		errs = append(errs, fmt.Errorf("these routes in the change have no journey step: %s; add steps or journeys for them (up to %d journeys)", strings.Join(names, ", "), maxJourneys))
	}
	if controls == 0 && len(sc.Routes) > len(inChange) {
		errs = append(errs, fmt.Errorf("add at least one journey of kind %q on a route outside the change, to catch app-wide slowdowns", model.KindControl))
	}
	if err := errors.Join(errs...); err != nil {
		return uncovered, err
	}
	return uncovered, nil
}

func routeKey(r model.Route) string { return r.Method + " " + r.Path }

func label(r model.Route) string {
	if r.Method == "" {
		return r.Path
	}
	return r.Method + " " + r.Path
}
