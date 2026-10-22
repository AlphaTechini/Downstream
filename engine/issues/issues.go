// Package issues builds one issue per regression from run.json, scan.json and
// notes.json, and reads back the hidden marker that makes an issue
// self-contained. The engine places the model's text; it never computes from it.
package issues

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"downstream/engine/model"
)

// Issue is one issue to open.
type Issue struct {
	FindingID string
	Title     string
	Body      string
}

const markerPrefix = "<!-- downstream:v1 "
const markerSuffix = " -->"

var (
	starterRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,255}$`)
	shaRe     = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// Build returns one issue per regressed step, ordered by headline finding ID.
// When no step regressed but the app process did, it returns one combined
// app issue. notes may be nil; each issue then says no notes were supplied.
func Build(run model.Run, scan *model.Scan, notes *model.Notes, mr int, starter string) ([]Issue, error) {
	starter = strings.TrimPrefix(starter, "@")
	if !starterRe.MatchString(starter) {
		return nil, fmt.Errorf("starter %q is not a valid username (letters, digits, '_', '.', '-', 1 to 255 characters)", starter)
	}
	if mr < 1 {
		return nil, fmt.Errorf("merge request number %d is not valid", mr)
	}
	if !shaRe.MatchString(run.Base) || !shaRe.MatchString(run.Head) {
		return nil, errors.New("run.json base and head must be 7 to 40 lowercase hex characters")
	}
	if notes != nil {
		if err := ValidateNotes(*notes, run); err != nil {
			return nil, err
		}
	}

	groups, app := regressionGroups(run)
	var out []Issue
	if len(groups) == 0 && len(app) > 0 {
		groups, app = [][]model.Finding{app}, nil
	}
	for _, g := range groups {
		var note *model.Note
		if notes != nil {
			if n, ok := notes.Items[g[0].ID]; ok {
				note = &n
			}
		}
		is, err := build(run, scan, note, g, app, mr, starter)
		if err != nil {
			return nil, err
		}
		out = append(out, is)
	}
	return out, nil
}

// headlineOrder and appOrder say which metric speaks for a step or the app.
var (
	headlineOrder = []string{model.MetricFailures, model.MetricP95, model.MetricMean, model.MetricP50}
	appOrder      = []string{model.MetricRSS, model.MetricCPU}
)

// Headlines returns the finding that names each issue Build would open: one per
// regressed step (failures, p95, mean, p50 in that order of preference), or, when
// no step regressed, one for the app process (peak RSS, then CPU). Notes are
// keyed by these IDs. The result is ordered by ID.
func Headlines(run model.Run) []model.Finding {
	groups, app := regressionGroups(run)
	if len(groups) == 0 && len(app) > 0 {
		groups = [][]model.Finding{app}
	}
	out := make([]model.Finding, 0, len(groups))
	for _, g := range groups {
		out = append(out, g[0])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// regressionGroups collects regression findings per (journey, step), headline
// first, and the app-wide regressions separately, also headline first. Groups
// are ordered by headline ID.
func regressionGroups(run model.Run) (groups [][]model.Finding, app []model.Finding) {
	type key struct{ journey, step string }
	by := map[key][]model.Finding{}
	for _, f := range run.Findings {
		if f.Class != model.ClassRegression {
			continue
		}
		if f.Journey == "" {
			app = append(app, f)
			continue
		}
		k := key{f.Journey, f.Step}
		by[k] = append(by[k], f)
	}
	for _, g := range by {
		groups = append(groups, rank(g, headlineOrder))
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i][0].ID < groups[j][0].ID })
	return groups, rank(app, appOrder)
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

func build(run model.Run, scan *model.Scan, note *model.Note, group, app []model.Finding, mr int, starter string) (Issue, error) {
	f := group[0]
	var j model.Journey
	label := "app"
	if f.Journey != "" {
		found := false
		for _, rj := range run.Journeys {
			if rj.ID == f.Journey {
				j, found = rj, true
				break
			}
		}
		if !found {
			return Issue{}, fmt.Errorf("finding %s: journey %q is not in run.json, so the issue cannot be made self-contained", f.ID, f.Journey)
		}
		label = oneLine(j.Goal)
		if label == "" {
			label = j.ID
		}
	}

	var fix []string
	if note != nil {
		fix = note.FixOptions
	}
	marker := model.Marker{
		Version: 1, MR: mr, Base: run.Base, Head: run.Head,
		FindingID: f.ID, Metric: f.Metric, Step: f.Step, Journey: j,
		B1: f.B1, C: f.C, B2: f.B2, Band: f.Band, FixOptions: fix,
	}
	if marker.FixOptions == nil {
		marker.FixOptions = []string{}
	}
	mline, err := encodeMarker(marker)
	if err != nil {
		return Issue{}, err
	}

	var b strings.Builder
	w := func(s string) { b.WriteString(s + "\n") }

	w("Run started by @" + starter + " on !" + strconv.Itoa(mr) + ".")
	w("")
	w("**Repro**")
	w("")
	if f.Journey != "" {
		for i, s := range j.Steps {
			w(strconv.Itoa(i+1) + ". " + code(s.Method+" "+s.Path) + ", expect " + strconv.Itoa(s.ExpectStatus))
		}
		w("")
		w("Rerun with:")
		w("")
		w("```")
		w("downstream run --base " + run.Base + " --head " + run.Head + " --journeys journeys.json --out run.json")
		w("```")
		w("")
		w("`journeys.json` is the journey embedded in this issue's hidden marker. Recover it with:")
		w("")
		w("```")
		w("downstream marker parse --journeys-out journeys.json < issue.md")
		w("```")
	} else {
		w("This figure covers the whole app process across every journey of the run, so the marker holds no single journey. " +
			"The journeys that were run are listed in `run.json` under `journeys`.")
		w("")
		w("```")
		w("downstream run --base " + run.Base + " --head " + run.Head + " --journeys journeys.json --out run.json")
		w("```")
	}
	w("")
	w("**Measured**")
	w("")
	if f.Journey != "" {
		w("- Step: " + code(f.Step))
	}
	for _, g := range group {
		measuredLines(w, g, "")
	}
	if len(app) > 0 {
		w("- App process:")
		for _, g := range app {
			measuredLines(w, g, "  ")
		}
	}
	w("")
	w("**Changed functions on this path**")
	w("")
	for _, l := range changedFuncs(run, scan, f) {
		w(l)
	}
	w("")
	w("**Likely cause**")
	w("")
	if note == nil {
		w("Not written: no notes were supplied for this finding.")
	} else {
		c := oneLine(note.LikelyCause)
		if !strings.HasPrefix(strings.ToLower(c), "likely") {
			c = "Likely: " + c
		}
		w(c)
	}
	w("")
	w("**Fix options**")
	w("")
	if len(fix) == 0 {
		w("None supplied.")
	}
	for i, o := range fix {
		w(strconv.Itoa(i+1) + ". " + oneLine(o))
	}
	w("")
	w("**How to reply**")
	w("")
	if len(fix) == 0 {
		w("No fix options were supplied, so there is nothing to apply.")
	} else {
		w("Reply with a mention of Downstream followed by `fix option N` in this issue to apply option N.")
	}
	w("")
	w(mline)

	return Issue{FindingID: f.ID, Title: title(label, f), Body: b.String()}, nil
}

func measuredLines(w func(string), f model.Finding, indent string) {
	w(indent + "- " + metricWords(f.Metric) + ": B1 " + figure(f.B1, f.Unit) + ", B2 " + figure(f.B2, f.Unit) +
		", C " + figure(f.C, f.Unit) + ", noise band " + figure(f.Band, f.Unit) +
		", change " + deltaWords(f) + " against the worse baseline")
	w(indent + "  - Rule: " + oneLine(f.Reason))
}

func title(label string, f model.Finding) string {
	dir := "up"
	if f.Delta < 0 {
		dir = "down"
	}
	var amt string
	switch f.Unit {
	case "count":
		amt = strconv.FormatFloat(abs(f.Delta), 'f', 0, 64) + " (was " + strconv.FormatFloat(max(f.B1, f.B2), 'f', 0, 64) + ")"
	default:
		amt = figure(abs(f.Delta), f.Unit)
		if max(f.B1, f.B2) != 0 {
			amt += " (" + signed(f.DeltaPct, 0) + "%)"
		}
	}
	return "[Downstream] " + label + ": " + metricWords(f.Metric) + " " + dir + " " + amt
}

func deltaWords(f model.Finding) string {
	dec := decimals(f.Unit)
	s := signed(f.Delta, dec)
	if f.Unit != "count" {
		s += " " + f.Unit
	}
	if max(f.B1, f.B2) != 0 {
		s += " (" + signed(f.DeltaPct, 0) + "%)"
	}
	return s
}

func metricWords(m string) string {
	switch m {
	case model.MetricP95:
		return "p95 latency"
	case model.MetricP50:
		return "p50 latency"
	case model.MetricMean:
		return "mean latency"
	case model.MetricFailures:
		return "failures"
	case model.MetricCPU:
		return "CPU time"
	case model.MetricRSS:
		return "peak memory"
	}
	return m
}

func decimals(unit string) int {
	switch unit {
	case "s":
		return 2
	case "count":
		return 0
	}
	return 1
}

func figure(v float64, unit string) string {
	s := strconv.FormatFloat(v, 'f', decimals(unit), 64)
	if unit == "count" {
		return s
	}
	return s + " " + unit
}

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

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func code(s string) string {
	return "`" + strings.ReplaceAll(oneLine(s), "`", "'") + "`"
}

// changedFuncs lists changed functions on routes the journey's steps hit
// (every kept journey for app-wide findings).
func changedFuncs(run model.Run, scan *model.Scan, f model.Finding) []string {
	if scan == nil {
		return []string{"Not available: no scan was supplied."}
	}
	var steps []model.Step
	for _, j := range run.Journeys {
		if f.Journey == "" || j.ID == f.Journey {
			steps = append(steps, j.Steps...)
		}
	}
	routesOf := map[string]map[string]bool{}
	for _, list := range [][]model.Impact{scan.Changed, scan.Affected} {
		for _, im := range list {
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
			rname := strings.TrimSpace(im.Method + " " + im.Path)
			for _, fn := range im.ChangedFuncs {
				if routesOf[fn] == nil {
					routesOf[fn] = map[string]bool{}
				}
				routesOf[fn][rname] = true
			}
		}
	}
	if len(routesOf) == 0 {
		return []string{"None: no changed function was found on the call path of the routes this journey calls."}
	}
	fns := make([]string, 0, len(routesOf))
	for fn := range routesOf {
		fns = append(fns, fn)
	}
	sort.Strings(fns)
	var out []string
	for _, fn := range fns {
		rs := make([]string, 0, len(routesOf[fn]))
		for r := range routesOf[fn] {
			rs = append(rs, code(r))
		}
		sort.Strings(rs)
		out = append(out, "- "+code(fn)+" on "+strings.Join(rs, ", "))
	}
	return out
}

// routeMatches reports whether a request (method, path) is served by a
// ServeMux-style pattern. {x} matches one segment, {x...} the rest, a trailing
// slash matches the subtree, {$} anchors the end. The query string is ignored.
func routeMatches(patMethod, pattern, method, path string) bool {
	if patMethod != "" && !strings.EqualFold(patMethod, method) {
		return false
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if !strings.HasPrefix(path, "/") || !strings.HasPrefix(pattern, "/") {
		return false
	}
	ps := strings.Split(pattern[1:], "/")
	xs := strings.Split(path[1:], "/")
	for i, p := range ps {
		last := i == len(ps)-1
		switch {
		case p == "" && last:
			// trailing slash: subtree match
			return true
		case p == "{$}" && last:
			return i < len(xs) && len(xs) == i+1 && xs[i] == ""
		case strings.HasPrefix(p, "{") && strings.HasSuffix(p, "...}"):
			return i < len(xs)
		case i >= len(xs):
			return false
		case strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}"):
			if xs[i] == "" {
				return false
			}
		case p != xs[i]:
			return false
		}
	}
	return len(ps) == len(xs)
}

// encodeMarker writes the marker comment. "--" may not appear inside an HTML
// comment, so each pair of hyphens is replaced by two JSON unicode escapes, which decode back to "--".
func encodeMarker(m model.Marker) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(m); err != nil {
		return "", err
	}
	js := strings.TrimSpace(buf.String())
	js = strings.ReplaceAll(js, "--", `\u002d\u002d`)
	return markerPrefix + js + markerSuffix, nil
}

// ParseMarker reads the hidden marker from an issue body (the last line that
// starts it), or accepts the bare marker JSON object, and validates the fields
// later steps depend on.
func ParseMarker(body string) (model.Marker, error) {
	var m model.Marker
	if t := strings.TrimSpace(body); strings.HasPrefix(t, "{") {
		return decodeMarker(t)
	}
	line := ""
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, markerPrefix) {
			line = l
		}
	}
	if line == "" {
		return m, errors.New("no downstream:v1 marker found in the issue body")
	}
	if !strings.HasSuffix(line, markerSuffix) {
		return m, errors.New("marker comment is not closed with -->")
	}
	return decodeMarker(strings.TrimSuffix(strings.TrimPrefix(line, markerPrefix), markerSuffix))
}

func decodeMarker(js string) (model.Marker, error) {
	var m model.Marker
	dec := json.NewDecoder(strings.NewReader(js))
	if err := dec.Decode(&m); err != nil {
		return model.Marker{}, fmt.Errorf("marker JSON is not valid: %w", err)
	}
	if dec.More() {
		return model.Marker{}, errors.New("marker JSON has trailing content")
	}
	switch {
	case m.Version != 1:
		return model.Marker{}, fmt.Errorf("marker version %d is not supported", m.Version)
	case m.MR < 1:
		return model.Marker{}, errors.New("marker has no merge request number")
	case !shaRe.MatchString(m.Base) || !shaRe.MatchString(m.Head):
		return model.Marker{}, errors.New("marker base and head must be 7 to 40 lowercase hex characters")
	case m.FindingID == "":
		return model.Marker{}, errors.New("marker has no finding ID")
	}
	return m, nil
}

// FixCheck returns the text of option (1-based) from the marker.
func FixCheck(m model.Marker, option int) (string, error) {
	if len(m.FixOptions) == 0 {
		return "", errors.New("this issue has no fix options")
	}
	if option < 1 || option > len(m.FixOptions) {
		return "", fmt.Errorf("option %d is out of range: this issue has options 1 to %d", option, len(m.FixOptions))
	}
	return m.FixOptions[option-1], nil
}

// Questions returns one markdown question per trade-off journey, for the
// merge request. Journeys follow run.Journeys order, then any others sorted.
func Questions(run model.Run) []string {
	byJourney := map[string][]model.Finding{}
	for _, f := range run.Findings {
		if f.Class == model.ClassTradeOff && f.Journey != "" {
			byJourney[f.Journey] = append(byJourney[f.Journey], f)
		}
	}
	var order []string
	seen := map[string]bool{}
	for _, j := range run.Journeys {
		if len(byJourney[j.ID]) > 0 && !seen[j.ID] {
			order = append(order, j.ID)
			seen[j.ID] = true
		}
	}
	var rest []string
	for id := range byJourney {
		if !seen[id] {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)
	order = append(order, rest...)

	goal := func(id string) string {
		for _, j := range run.Journeys {
			if j.ID == id && oneLine(j.Goal) != "" {
				return oneLine(j.Goal)
			}
		}
		return id
	}

	var out []string
	for _, id := range order {
		fs := byJourney[id]
		sort.Slice(fs, func(i, j int) bool { return fs[i].ID < fs[j].ID })
		var better, worse []string
		for _, f := range fs {
			line := "- " + code(f.Step) + " " + metricWords(f.Metric) + ": " + deltaWords(f)
			if f.Delta > 0 {
				worse = append(worse, line)
			} else {
				better = append(better, line)
			}
		}
		var b strings.Builder
		b.WriteString("**Question for the reviewers:** on " + goal(id) +
			", some latency figures got better and others got worse. Is that acceptable for this change? No issue opened.\n")
		b.WriteString("\nBetter (change against the worse baseline run):\n\n" + strings.Join(better, "\n") + "\n")
		b.WriteString("\nWorse (change against the worse baseline run):\n\n" + strings.Join(worse, "\n") + "\n")
		out = append(out, b.String())
	}
	return out
}

var (
	figureRe = regexp.MustCompile(`\d+(\.\d+)?\s*%|\d+(\.\d+)?\s*(ms|MB|s)\b`)
	tableRe  = regexp.MustCompile(`(?m)^\s*\|`)
)

// ValidateNotes checks notes.json before any of it is placed in an issue. Keys
// must be headline finding IDs (see Headlines).
func ValidateNotes(n model.Notes, run model.Run) error {
	headline := map[string]bool{}
	for _, f := range Headlines(run) {
		headline[f.ID] = true
	}
	known := map[string]model.Finding{}
	for _, f := range run.Findings {
		known[f.ID] = f
	}
	keys := make([]string, 0, len(n.Items))
	for k := range n.Items {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		note := n.Items[k]
		f, ok := known[k]
		switch {
		case !ok:
			return fmt.Errorf("notes: %q is not a finding ID in this run", k)
		case f.Class != model.ClassRegression:
			return fmt.Errorf("notes: %q is not a regression; notes are only for regressions", k)
		case !headline[k]:
			return fmt.Errorf("notes: %q is not a headline finding; one issue is opened per regressed step and notes are keyed by its headline finding (see Headlines)", k)
		}
		if l := utf8.RuneCountInString(strings.TrimSpace(note.LikelyCause)); l < 1 || l > 600 {
			return fmt.Errorf("notes: %q: likely_cause must be 1 to 600 characters, got %d", k, l)
		}
		if err := checkText(k, "likely_cause", note.LikelyCause); err != nil {
			return err
		}
		if len(note.FixOptions) < 1 || len(note.FixOptions) > 5 {
			return fmt.Errorf("notes: %q: fix_options must have 1 to 5 entries, got %d", k, len(note.FixOptions))
		}
		for i, o := range note.FixOptions {
			where := "fix_options[" + strconv.Itoa(i) + "]"
			if l := utf8.RuneCountInString(strings.TrimSpace(o)); l < 1 || l > 400 {
				return fmt.Errorf("notes: %q: %s must be 1 to 400 characters, got %d", k, where, l)
			}
			if err := checkText(k, where, o); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkText(id, field, s string) error {
	for _, bad := range []string{"<!--", "-->", "```"} {
		if strings.Contains(s, bad) {
			return fmt.Errorf("notes: %q: %s contains %q, which is not allowed", id, field, bad)
		}
	}
	if tableRe.MatchString(s) {
		return fmt.Errorf("notes: %q: %s has a line starting with \"|\"; tables are rendered by the engine, not written by hand", id, field)
	}
	if m := figureRe.FindString(s); m != "" {
		return fmt.Errorf("notes: %q: %s contains the figure %q; figures come from the engine's measurements, so describe the cause without numbers", id, field, m)
	}
	return nil
}
