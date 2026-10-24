package issues

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"downstream/engine/model"
)

// maxIssues caps how many issues one run opens.
const maxIssues = 5

// maxTitle is the longest title Build produces, in characters.
const maxTitle = 199

// headlineOrder says which metric speaks for a step. appOrder is the fallback
// order for app-wide findings that carry no tolerance (a run.json written
// before gating).
var (
	headlineOrder = []string{model.MetricFailures, model.MetricP95, model.MetricMean, model.MetricP50}
	appOrder      = []string{model.MetricRSS, model.MetricCPU}
)

// Group is what one issue covers: either the failing steps that share changed
// functions on the routes they call, or the app-wide gated regressions.
type Group struct {
	Key      []string        // changed functions shared by the steps, sorted; empty for the "outside" group
	Scoped   bool            // a scan was available, so Key is meaningful
	Headline model.Finding   // names the issue; notes are keyed by its ID
	Steps    []model.Finding // each regressed step's headline finding, ordered by ID
	App      []model.Finding // the app issue's gated regressions, largest change against its tolerance first
	Advisory []model.Finding // advisory regressions listed in this issue, by journey, step and metric
	AppIssue bool            // the one issue for app-wide gated regressions

	routes map[string][]string // changed function -> routes (METHOD path) that carry it
}

// Plan says which issues Build opens and how the regressed steps were grouped.
type Plan struct {
	Groups         []Group // step groups with the most steps first, then the app issue; at most maxIssues
	RegressedSteps int
	TotalGroups    int // groups of regressed steps before the cap
	LeftOutGroups  int
	LeftOutSteps   int
	AppRegressions int // gated app-wide regressions, all listed in the app issue
}

// Headlines returns the headline finding of every issue Build would open, in
// issue order. Notes are keyed by these IDs.
func Headlines(run model.Run, scan *model.Scan) []model.Finding {
	plan := PlanIssues(run, scan)
	out := make([]model.Finding, 0, len(plan.Groups))
	for _, g := range plan.Groups {
		out = append(out, g.Headline)
	}
	return out
}

// AdvisoryRegressions returns the regression-class findings that may not open
// an issue (latency), ordered by ID.
func AdvisoryRegressions(run model.Run) []model.Finding {
	var out []model.Finding
	for _, f := range run.Findings {
		if f.Class == model.ClassRegression && !f.Decides() {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Legacy reports whether run.json was written before findings carried a Kind.
// Such a run has no gated or advisory split, so it is planned the way it was
// then: app-wide findings join the largest step group.
func Legacy(run model.Run) bool {
	if len(run.Findings) == 0 {
		return false
	}
	for _, f := range run.Findings {
		if f.Kind != "" {
			return false
		}
	}
	return true
}

// PlanIssues groups the regressed steps. A step's cause key is the sorted,
// de-duplicated list of changed functions (scan.Changed) on the routes it
// calls. Without a scan every step is in one group. Only gated findings open
// issues. The gated app-wide regressions (counters, CPU per journey, memory)
// are measured per process, not per journey, so they form one more issue
// instead of joining a step group. Advisory regressions open nothing; they are
// listed in the issue that already covers their journey, or in the app issue.
// A legacy run (see Legacy) has no such split: its app-wide findings join the
// largest step group, or form their own issue when no step regressed.
func PlanIssues(run model.Run, scan *model.Scan) Plan {
	type skey struct{ journey, step string }
	byStep := map[skey][]model.Finding{}
	var app []model.Finding
	advisoryByJourney := map[string][]model.Finding{}
	var advisoryApp []model.Finding
	for _, f := range run.Findings {
		if f.Class != model.ClassRegression {
			continue
		}
		switch {
		case !f.Decides() && f.Journey == "":
			advisoryApp = append(advisoryApp, f)
		case !f.Decides():
			advisoryByJourney[f.Journey] = append(advisoryByJourney[f.Journey], f)
		case f.Journey == "":
			app = append(app, f)
		default:
			k := skey{f.Journey, f.Step}
			byStep[k] = append(byStep[k], f)
		}
	}

	stepDefs := map[skey]model.Step{}
	for _, j := range run.Journeys {
		for _, s := range j.Steps {
			stepDefs[skey{j.ID, s.Name}] = s
		}
	}

	groups := map[string]*Group{}
	var keys []string
	for k, fs := range byStep {
		head := rank(fs, headlineOrder)[0]
		var funcs []string
		var routes map[string][]string
		if scan != nil {
			if def, ok := stepDefs[k]; ok {
				funcs, routes = changedOn(scan, []model.Step{def})
			}
		}
		id := strings.Join(funcs, "\x00")
		g := groups[id]
		if g == nil {
			g = &Group{Key: funcs, Scoped: scan != nil, routes: map[string][]string{}}
			groups[id] = g
			keys = append(keys, id)
		}
		g.Steps = append(g.Steps, head)
		for fn, rs := range routes {
			g.routes[fn] = mergeSorted(g.routes[fn], rs)
		}
	}

	var all []Group
	for _, id := range keys {
		g := groups[id]
		sort.Slice(g.Steps, func(i, j int) bool { return g.Steps[i].ID < g.Steps[j].ID })
		g.Headline = pickHeadline(g.Steps)
		all = append(all, *g)
	}
	sort.Slice(all, func(i, j int) bool {
		if len(all[i].Steps) != len(all[j].Steps) {
			return len(all[i].Steps) > len(all[j].Steps)
		}
		return all[i].Headline.ID < all[j].Headline.ID
	})

	plan := Plan{RegressedSteps: len(byStep), TotalGroups: len(all), AppRegressions: len(app)}
	legacy := Legacy(run)
	room := maxIssues
	if len(app) > 0 && !legacy {
		room--
	}
	if len(all) > room {
		for _, g := range all[room:] {
			plan.LeftOutGroups++
			plan.LeftOutSteps += len(g.Steps)
		}
		all = all[:room]
	}

	// A journey's advisory regressions go in the first issue that has one of
	// its steps; the app issue takes the rest.
	shown := map[string]bool{}
	for i := range all {
		var journeys []string
		for _, s := range all[i].Steps {
			if !shown[s.Journey] && len(advisoryByJourney[s.Journey]) > 0 {
				shown[s.Journey] = true
				journeys = append(journeys, s.Journey)
			}
		}
		sort.Strings(journeys)
		for _, j := range journeys {
			all[i].Advisory = append(all[i].Advisory, advisoryByJourney[j]...)
		}
	}
	plan.Groups = all

	if len(app) > 0 {
		sort.SliceStable(app, func(i, j int) bool { return appBefore(app[i], app[j]) })
		if legacy && len(plan.Groups) > 0 {
			plan.Groups[0].App = app
			app = nil
		}
	}
	if len(app) > 0 {
		g := Group{Scoped: scan != nil, Headline: app[0], App: app, AppIssue: !legacy, routes: map[string][]string{}}
		if scan != nil {
			var steps []model.Step
			for _, j := range run.Journeys {
				steps = append(steps, j.Steps...)
			}
			g.Key, g.routes = changedOn(scan, steps)
		}
		var rest []string
		for j := range advisoryByJourney {
			if !shown[j] {
				rest = append(rest, j)
			}
		}
		sort.Strings(rest)
		for _, j := range rest {
			g.Advisory = append(g.Advisory, advisoryByJourney[j]...)
		}
		g.Advisory = append(g.Advisory, advisoryApp...)
		plan.Groups = append(plan.Groups, g)
	}
	for i := range plan.Groups {
		a := plan.Groups[i].Advisory
		sort.SliceStable(a, func(x, y int) bool {
			if a[x].Journey != a[y].Journey {
				return a[x].Journey < a[y].Journey
			}
			return a[x].ID < a[y].ID
		})
	}
	return plan
}

// appBefore orders app-wide regressions: the largest change against its own
// tolerance first. Findings without a tolerance (from before gating) follow
// the fixed metric order instead.
func appBefore(a, b model.Finding) bool {
	if a.Allowed > 0 && b.Allowed > 0 {
		ra, rb := a.Delta/a.Allowed, b.Delta/b.Allowed
		if ra != rb {
			return ra > rb
		}
		return a.ID < b.ID
	}
	pos := func(m string) int {
		for i, o := range appOrder {
			if o == m {
				return i
			}
		}
		return len(appOrder)
	}
	if pa, pb := pos(a.Metric), pos(b.Metric); pa != pb {
		return pa < pb
	}
	return a.ID < b.ID
}

// changedOn returns the changed functions (sorted, unique) on the scan's changed
// routes that any of the steps call, and the routes carrying each function.
func changedOn(scan *model.Scan, steps []model.Step) ([]string, map[string][]string) {
	routes := map[string][]string{}
	for _, im := range scan.Changed {
		if len(im.ChangedFuncs) == 0 {
			continue
		}
		hit := false
		for _, s := range steps {
			if routeMatches(im.Method, im.Path, s.Method, s.Path) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		name := strings.TrimSpace(im.Method + " " + im.Path)
		for _, fn := range im.ChangedFuncs {
			routes[fn] = mergeSorted(routes[fn], []string{name})
		}
	}
	funcs := make([]string, 0, len(routes))
	for fn := range routes {
		funcs = append(funcs, fn)
	}
	sort.Strings(funcs)
	return funcs, routes
}

func mergeSorted(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string(nil), a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// pickHeadline prefers a failures finding, then the largest absolute change
// (so the title names the step that got slowest in milliseconds, not the one
// with the biggest ratio on a tiny baseline), then the largest percentage
// change, then the lowest ID.
func pickHeadline(steps []model.Finding) model.Finding {
	best := steps[0]
	for _, f := range steps[1:] {
		if better(f, best) {
			best = f
		}
	}
	return best
}

func better(a, b model.Finding) bool {
	af, bf := a.Metric == model.MetricFailures, b.Metric == model.MetricFailures
	switch {
	case af != bf:
		return af
	case a.Delta != b.Delta:
		return a.Delta > b.Delta
	case a.DeltaPct != b.DeltaPct:
		return a.DeltaPct > b.DeltaPct
	}
	return a.ID < b.ID
}

// rank orders findings by the preferred metric order; metrics not in the list
// follow, by ID, so no regression is dropped.
func rank(fs []model.Finding, order []string) []model.Finding {
	pos := func(m string) int {
		for i, o := range order {
			if o == m {
				return i
			}
		}
		return len(order)
	}
	out := append([]model.Finding(nil), fs...)
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := pos(out[i].Metric), pos(out[j].Metric)
		if pi != pj {
			return pi < pj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (g Group) otherJourneys(journey string) bool {
	for _, s := range g.Steps {
		if s.Journey != journey {
			return true
		}
	}
	return false
}

func changedFuncLines(g Group, scan *model.Scan) []string {
	if scan == nil {
		return []string{"Not available: no scan was supplied."}
	}
	if len(g.Key) == 0 {
		return []string{"None: no changed function is on the call path of the routes these steps call."}
	}
	var out []string
	for _, fn := range g.Key {
		rs := make([]string, 0, len(g.routes[fn]))
		for _, r := range g.routes[fn] {
			rs = append(rs, code(r))
		}
		out = append(out, "- "+code(fn)+" on "+strings.Join(rs, ", "))
	}
	return out
}

// title names the issue. A single step or the app alone keeps the short form;
// several steps are named by their shared cause and the worst one.
func (g Group) title(run model.Run) string {
	f := g.Headline
	n := len(g.Steps)
	if n <= 1 {
		label := "app"
		if f.Journey != "" {
			label = f.Journey
			for _, j := range run.Journeys {
				if j.ID == f.Journey && oneLine(j.Goal) != "" {
					label = oneLine(j.Goal)
				}
			}
		}
		return fitTitle("[Downstream] "+label+": "+metricWords(f.Metric)+" "+amountWords(f), nil)
	}
	head := "[Downstream] " + strconv.Itoa(n) + " steps " + g.stepVerb()
	tail := ": worst " + f.Journey + "." + f.Step + " " + metricWords(f.Metric) + " " + amountWords(f)
	switch {
	case !g.Scoped:
		return fitTitle(head+tail, nil)
	case len(g.Key) == 0:
		return fitTitle(head+" on routes outside the diff's call paths"+tail, nil)
	}
	return fitTitle(head+" via ", func(prefix string) string {
		for k := len(g.Key); k >= 1; k-- {
			list := strings.Join(g.Key[:k], ", ")
			if k < len(g.Key) {
				list += " and " + strconv.Itoa(len(g.Key)-k) + " more"
			}
			if t := prefix + list + tail; utf8.RuneCountInString(t) <= maxTitle {
				return t
			}
		}
		return prefix + g.Key[0] + tail
	})
}

// stepVerb is "failing" when every step in the group regressed on failures,
// and "slower" for the latency regressions of a run.json from before gating.
func (g Group) stepVerb() string {
	for _, s := range g.Steps {
		if s.Metric != model.MetricFailures {
			return "slower"
		}
	}
	return "failing"
}

// fitTitle returns s, or what build makes of it, cut to maxTitle characters.
func fitTitle(s string, build func(string) string) string {
	if build != nil {
		s = build(s)
	}
	if utf8.RuneCountInString(s) <= maxTitle {
		return s
	}
	r := []rune(s)
	return string(r[:maxTitle-3]) + "..."
}
