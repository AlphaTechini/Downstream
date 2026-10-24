// Package issues builds the issues for the regressions in run.json, scan.json and
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
	FindingID string   `json:"finding_id"` // the group's headline finding
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Steps     []string `json:"steps"` // finding IDs covered: each step's headline
}

// The marker is a plain line in a fenced block. It used to be an HTML comment,
// but GitLab's flow tools drop HTML comments, so issue #4 lost it (session
// 9999905). Old comment markers are still read.
const markerPrefix = "downstream:v1 "
const markerSuffix = ""

const legacyPrefix = "<!-- downstream:v1 "
const legacySuffix = " -->"

var (
	starterRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,255}$`)
	shaRe     = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// Build returns the issues for the run: regressed steps are grouped by the
// changed functions on the routes they call, and each group becomes one issue
// (at most maxIssues, the groups with the most steps first). When no step
// regressed but the app process did, there is one app issue. notes may be nil;
// each issue then says no notes were supplied.
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
		if err := ValidateNotes(*notes, run, scan); err != nil {
			return nil, err
		}
	}

	plan := PlanIssues(run, scan)
	out := make([]Issue, 0, len(plan.Groups))
	for _, g := range plan.Groups {
		var note *model.Note
		if notes != nil {
			if n, ok := notes.Items[g.Headline.ID]; ok {
				note = &n
			}
		}
		is, err := build(run, scan, note, g, mr, starter)
		if err != nil {
			return nil, err
		}
		out = append(out, is)
	}
	return out, nil
}

func build(run model.Run, scan *model.Scan, note *model.Note, g Group, mr int, starter string) (Issue, error) {
	f := g.Headline
	var j model.Journey
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
	w("**Changed functions**")
	w("")
	for _, l := range changedFuncLines(g, scan) {
		w(l)
	}
	w("")
	w("**Measured**")
	w("")
	if len(g.Steps) > 0 {
		w("| Journey | Step | Metric | B1 | B2 | C | Noise band | Change |")
		w("|---|---|---|---|---|---|---|---|")
		for _, s := range g.Steps {
			w("| " + cell(s.Journey) + " | " + cell(s.Step) + " | " + metricWords(s.Metric) +
				" | " + figure(s.B1, s.Unit) + " | " + figure(s.B2, s.Unit) + " | " + figure(s.C, s.Unit) +
				" | " + figure(s.Band, s.Unit) + " | " + cell(deltaWords(s)) + " |")
		}
		w("")
		w("Each row is the step's headline regression (failures, then p95, mean, p50). Change is against the worse baseline run.")
		w("")
	}
	if len(g.App) > 0 {
		w("| Metric | B1 | B2 | C | Change | Allowed | Noise band | Smallest detectable |")
		w("|---|---|---|---|---|---|---|---|")
		for _, a := range g.App {
			w("| " + metricWords(a.Metric) + " | " + figure(a.B1, a.Unit) + " | " + figure(a.B2, a.Unit) + " | " + figure(a.C, a.Unit) +
				" | " + cell(deltaWords(a)) + " | " + allowedWords(a) + " | " + figure(a.Band, a.Unit) +
				" | " + cell(detectableWords(a)) + " |")
		}
		w("")
		w("These are measured on the whole app process, not per journey. Change is against the worse baseline run. " +
			"Allowed is the tolerance for the metric. A rise counts only when it is more than both the allowed figure and the noise band. " +
			"Smallest detectable is the larger of the two: the smallest change this run could have told apart from noise.")
		w("")
	}
	advisoryLines(w, g.Advisory, run)
	w("Rule for the headline (" + code(f.ID) + "): " + oneLine(f.Reason))
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
		w("`journeys.json` is the journey embedded in this issue's hidden marker (the journey of the headline step). Recover it with:")
		w("")
		w("```")
		w("downstream marker parse --journeys-out journeys.json < issue.md")
		w("```")
		if g.otherJourneys(f.Journey) {
			w("")
			w("Other steps in the table belong to other journeys of the run; their definitions are in `run.json` under `journeys`.")
		}
	} else {
		w("This figure covers the whole app process across every journey of the run, so the marker holds no single journey. " +
			"The journeys that were run are listed in `run.json` under `journeys`.")
		w("")
		w("```")
		w("downstream run --base " + run.Base + " --head " + run.Head + " --journeys journeys.json --out run.json")
		w("```")
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
		w("Reply with a mention of Downstream followed by `fix it` to apply option 1, or `fix option N` for another option. " +
			"Downstream commits the change on a new branch, opens a merge request for it against the original branch, " +
			"and re-runs this journey on it. It never merges.")
	}
	w("")
	w("<details><summary>Downstream state, read by fix it and verify. Do not edit.</summary>")
	w("")
	w("```text")
	w(mline)
	w("```")
	w("")
	w("</details>")

	ids := make([]string, 0, len(g.Steps))
	for _, s := range g.Steps {
		ids = append(ids, s.ID)
	}
	for _, a := range g.App {
		ids = append(ids, a.ID)
	}
	if len(ids) == 0 {
		ids = append(ids, f.ID)
	}
	sort.Strings(ids)
	return Issue{FindingID: f.ID, Title: g.title(run), Body: b.String(), Steps: ids}, nil
}

// advisoryLines writes the latency regressions that share the issue, per
// journey. They are not used for verdicts and open nothing on their own.
func advisoryLines(w func(string), fs []model.Finding, run model.Run) {
	if len(fs) == 0 {
		return
	}
	w("**Latency on the same run (advisory)**")
	w("")
	w("Latency is not used for verdicts: it varies too much between runs of identical code. " +
		"These figures rose on this run and are listed for context only. Each step shows one figure: p95, else mean, else p50.")
	w("")
	// One row per step: its p95 if that rose, else mean, else p50.
	chosen := map[string]model.Finding{}
	for _, f := range rank(fs, headlineOrder) {
		k := f.Journey + "\x00" + f.Step
		if _, ok := chosen[k]; !ok {
			chosen[k] = f
		}
	}
	var order []string
	byJourney := map[string][]model.Finding{}
	emitted := map[string]bool{}
	for _, f := range fs {
		k := f.Journey + "\x00" + f.Step
		if emitted[k] {
			continue
		}
		emitted[k] = true
		if _, ok := byJourney[f.Journey]; !ok {
			order = append(order, f.Journey)
		}
		byJourney[f.Journey] = append(byJourney[f.Journey], chosen[k])
	}
	for _, j := range order {
		label := "App process"
		if j != "" {
			label = j
			for _, rj := range run.Journeys {
				if rj.ID == j && oneLine(rj.Goal) != "" {
					label = oneLine(rj.Goal)
				}
			}
		}
		w("*" + cell(label) + "*")
		w("")
		w("| Step | Metric | B1 | B2 | C | Noise band | Change | Smallest detectable |")
		w("|---|---|---|---|---|---|---|---|")
		for _, f := range byJourney[j] {
			step := f.Step
			if step == "" {
				step = "-"
			}
			w("| " + cell(step) + " | " + metricWords(f.Metric) + " | " + figure(f.B1, f.Unit) + " | " + figure(f.B2, f.Unit) +
				" | " + figure(f.C, f.Unit) + " | " + figure(f.Band, f.Unit) + " | " + cell(deltaWords(f)) +
				" | " + cell(detectableWords(f)) + " |")
		}
		w("")
	}
}

// allowedWords is the tolerance as a figure, or n/a for a finding from before
// gating, which carries none.
func allowedWords(f model.Finding) string {
	if f.Allowed == 0 {
		return "n/a"
	}
	return figure(f.Allowed, f.Unit)
}

// detectableWords is "5.00 ms/journey (10.0%)": the smallest change the run
// could tell from noise, and its share of the worse baseline when that is not
// zero.
func detectableWords(f model.Finding) string {
	if f.Detectable == 0 {
		return "n/a"
	}
	s := figure(f.Detectable, f.Unit)
	if f.DetectablePct != 0 {
		s += " (" + strconv.FormatFloat(f.DetectablePct, 'f', 1, 64) + "%)"
	}
	return s
}

func cell(s string) string {
	return strings.ReplaceAll(oneLine(s), "|", `\|`)
}

// amountWords is "up 410.5 ms (+853%)" or "up 30 (was 0)".
func amountWords(f model.Finding) string {
	dir := "up"
	if f.Delta < 0 {
		dir = "down"
	}
	if f.Unit == "count" {
		return dir + " " + strconv.FormatFloat(abs(f.Delta), 'f', 0, 64) + " (was " + strconv.FormatFloat(max(f.B1, f.B2), 'f', 0, 64) + ")"
	}
	amt := figure(abs(f.Delta), f.Unit)
	if max(f.B1, f.B2) != 0 {
		amt += " (" + signed(f.DeltaPct, 0) + "%)"
	}
	return dir + " " + amt
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
	case model.MetricCPUPerJourney:
		return "CPU per journey"
	case model.MetricWriteBytesPerJourney:
		return "bytes written per journey"
	case model.MetricWriteSyscallsPerJourney:
		return "write syscalls per journey"
	case model.MetricOpenFDs:
		return "open file descriptors"
	case model.MetricThreads:
		return "threads"
	case model.MetricAllocObjectsPerJourney:
		return "heap objects allocated per journey"
	case model.MetricAllocBytesPerJourney:
		return "heap bytes allocated per journey"
	case model.MetricGCCycles:
		return "GC cycles per pass"
	case model.MetricGCCyclesPerJourney:
		return "GC cycles per journey"
	case model.MetricGoroutines:
		return "goroutines"
	}
	return m
}

func decimals(unit string) int {
	switch unit {
	case "s", "ms/journey", "calls/journey":
		return 2
	case "GC cycles/journey":
		return 4
	case "count", "B/journey", "objects/journey", "cycles":
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

// encodeMarker writes the marker line. Backticks are escaped so the JSON cannot
// close the fenced block around it, and "--" so it stays safe to paste into an
// HTML comment; both escapes decode back to the original text.
func encodeMarker(m model.Marker) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(m); err != nil {
		return "", err
	}
	js := strings.TrimSpace(buf.String())
	js = strings.ReplaceAll(js, "--", `\u002d\u002d`)
	js = strings.ReplaceAll(js, "`", `\u0060`)
	return markerPrefix + js + markerSuffix, nil
}

// ParseMarker reads the marker from an issue body (the last line that starts
// it), or accepts the bare marker JSON object, and validates the fields later
// steps depend on.
func ParseMarker(body string) (model.Marker, error) {
	var m model.Marker
	if t := strings.TrimSpace(body); strings.HasPrefix(t, "{") {
		return decodeMarker(t)
	}
	line, legacy := "", false
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, markerPrefix):
			line, legacy = l, false
		case strings.HasPrefix(l, legacyPrefix):
			line, legacy = l, true
		}
	}
	if line == "" {
		return m, errors.New("no downstream:v1 marker found in the issue body")
	}
	if legacy {
		if !strings.HasSuffix(line, legacySuffix) {
			return m, errors.New("marker comment is not closed with -->")
		}
		return decodeMarker(strings.TrimSuffix(strings.TrimPrefix(line, legacyPrefix), legacySuffix))
	}
	return decodeMarker(strings.TrimPrefix(line, markerPrefix))
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

// Questions returns one markdown question per trade-off, for the merge
// request: one for each journey that has one (a run.json from before gating),
// then one for the app process. Journeys follow run.Journeys order, then any
// others sorted.
func Questions(run model.Run) []string {
	byJourney := map[string][]model.Finding{}
	for _, f := range run.Findings {
		if f.Class == model.ClassTradeOff {
			byJourney[f.Journey] = append(byJourney[f.Journey], f)
		}
	}
	var order []string
	seen := map[string]bool{"": true}
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
	if len(byJourney[""]) > 0 {
		order = append(order, "")
	}

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
			line := "- " + metricWords(f.Metric) + ": " + deltaWords(f)
			if f.Step != "" {
				line = "- " + code(f.Step) + " " + metricWords(f.Metric) + ": " + deltaWords(f)
			}
			if f.Delta > 0 {
				worse = append(worse, line)
			} else {
				better = append(better, line)
			}
		}
		var b strings.Builder
		if id == "" {
			b.WriteString("**Question for the reviewers:** on the app process, some gated figures got better and others got worse. " +
				"Is that acceptable for this change? No issue opened.\n")
		} else {
			b.WriteString("**Question for the reviewers:** on " + goal(id) +
				", some latency figures got better and others got worse. Is that acceptable for this change? No issue opened.\n")
		}
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
func ValidateNotes(n model.Notes, run model.Run, scan *model.Scan) error {
	headline := map[string]bool{}
	for _, f := range Headlines(run, scan) {
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
			return fmt.Errorf("notes: %q is not a headline finding; issues are opened per group of regressed steps and notes are keyed by the group's headline finding (see Headlines)", k)
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
