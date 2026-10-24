package issues

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"downstream/engine/model"
)

// hashPart matches a content hash before the extension, as fecheck strips it:
// index-BQx1u_j8.js -> index.js.
var hashPart = regexp.MustCompile(`[-.][A-Za-z0-9_]{8,}(\.[a-z0-9]+)$`)

// feCause is one root cause and every journey it showed up in.
type feCause struct {
	key, title string
	lines      []string
	journeys   []string
}

// id is the cause's finding ID: short and stable for the same frontend.json,
// so the model can key its notes on it.
func (c *feCause) id() string {
	sum := sha1.Sum([]byte(c.key))
	return "fe-" + hex.EncodeToString(sum[:4])
}

// frontendCauses groups the frontend regressions by root cause: the same
// culprit asset, missing URL, accessibility or markup rule, or JS error seen
// on several journeys is one cause. Withheld and improved checks are left out.
func frontendCauses(f model.Frontend) []*feCause {
	if f.Status != model.FrontendChecked || f.Compare == nil {
		return nil
	}
	causes := map[string]*feCause{}
	add := func(key, title, journey, line string) {
		c, ok := causes[key]
		if !ok {
			c = &feCause{key: key, title: title}
			causes[key] = c
		}
		if !slices.Contains(c.journeys, journey) {
			c.journeys = append(c.journeys, journey)
		}
		c.lines = append(c.lines, "- `"+journey+"`: "+line)
	}
	for _, j := range f.Compare.Journeys {
		for _, x := range j.Findings {
			if x.Verdict == "regression" {
				feCauses(j.ID, x, add)
			}
		}
	}
	out := make([]*feCause, 0, len(causes))
	for _, c := range causes {
		out = append(out, c)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].key < out[k].key })
	return out
}

// FrontendIDs lists the finding IDs of the frontend issues, the keys the
// model's notes use for them.
func FrontendIDs(f model.Frontend) []string {
	var ids []string
	for _, c := range frontendCauses(f) {
		ids = append(ids, c.id())
	}
	return ids
}

// ValidateFrontendNotes checks the notes keyed by frontend finding IDs with the
// same limits as backend notes.
func ValidateFrontendNotes(n model.Notes, f model.Frontend) error {
	ids := FrontendIDs(f)
	keys := make([]string, 0, len(n.Items))
	for k := range n.Items {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !slices.Contains(ids, k) {
			return fmt.Errorf("notes: %q is not a frontend finding ID in this run", k)
		}
		if err := checkNote(k, n.Items[k]); err != nil {
			return err
		}
	}
	return nil
}

// pickJourneys returns the page journeys with the given IDs from fecheck's
// validated journeys, as a JSON array.
func pickJourneys(all json.RawMessage, ids []string) json.RawMessage {
	var js []json.RawMessage
	if json.Unmarshal(all, &js) != nil {
		return nil
	}
	var keep []json.RawMessage
	for _, j := range js {
		var head struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(j, &head) == nil && slices.Contains(ids, head.ID) {
			keep = append(keep, j)
		}
	}
	if len(keep) == 0 {
		return nil
	}
	b, err := json.Marshal(keep)
	if err != nil {
		return nil
	}
	return b
}

// FrontendIssues opens one issue per root cause among the frontend
// regressions. With notes, an issue carries the likely cause, numbered fix
// options and a state block that fix it reads.
func FrontendIssues(f model.Frontend, notes *model.Notes, mr int, starter string) ([]Issue, error) {
	var out []Issue
	for _, c := range frontendCauses(f) {
		id := c.id()
		var note *model.Note
		if notes != nil {
			if n, ok := notes.Items[id]; ok {
				note = &n
			}
		}
		fix := []string{}
		if note != nil {
			fix = note.FixOptions
		}
		marker := model.Marker{
			Version: 1, MR: mr, Base: f.Base, Head: f.Head, FindingID: id, Metric: c.title,
			FixOptions: fix, Kind: model.MarkerFrontend,
			FEJourneys: pickJourneys(f.JourneysUsed, c.journeys), Files: f.ChangedFiles,
		}
		mline, err := encodeMarker(marker)
		if err != nil {
			return nil, err
		}
		var b strings.Builder
		w := func(s string) { b.WriteString(s + "\n") }
		w(fmt.Sprintf("Downstream's frontend check on !%d found this on the head build (%s) and not on the base build (%s). Started by @%s.", mr, short(f.Head), short(f.Base), starter))
		w("")
		w("**Where it shows up**")
		w("")
		for _, l := range c.lines {
			w(l)
		}
		w("")
		w("Pages were rendered in happy-dom with the app's own JavaScript, three times: base twice, then head. A check that moved between the two base runs is withheld, not reported here.")
		w("")
		if len(f.ChangedFiles) > 0 {
			w("**Frontend files changed**")
			w("")
			for _, p := range f.ChangedFiles {
				w("- " + code(p))
			}
			w("")
		}
		w("Not checked: " + strings.Join(f.NotChecked, ", ") + ".")
		w("")
		w("**Likely cause**")
		w("")
		if note == nil {
			w("Not written: no notes were supplied for this finding.")
		} else {
			lc := oneLine(note.LikelyCause)
			if !strings.HasPrefix(strings.ToLower(lc), "likely") {
				lc = "Likely: " + lc
			}
			w(lc)
		}
		w("")
		w("**Fix options**")
		w("")
		if len(fix) == 0 {
			w("None supplied.")
		}
		for i, o := range fix {
			w(fmt.Sprintf("%d. %s", i+1, oneLine(o)))
		}
		w("")
		w("**How to reply**")
		w("")
		if len(fix) == 0 {
			w("No fix options were supplied, so there is nothing to apply.")
		} else {
			w("Reply with a mention of Downstream followed by `fix it` to apply option 1, or `fix option N` for another option. " +
				"Downstream commits the change on a new branch, opens a merge request for it against the original branch, " +
				"and re-runs the page check on it. It never merges.")
		}
		w("")
		w("<details><summary>Downstream state, read by fix it. Do not edit.</summary>")
		w("")
		w("```text")
		w(mline)
		w("```")
		w("")
		w("</details>")
		out = append(out, Issue{FindingID: id, Title: "[Downstream] frontend: " + c.title, Body: b.String(), Steps: c.journeys})
	}
	return out, nil
}

// feCauses splits one finding into its root causes.
func feCauses(journey string, x model.FEFinding, add func(key, title, journey, line string)) {
	switch x.Metric {
	case "missing assets":
		var d []struct {
			URL    string `json:"url"`
			Status int    `json:"status"`
		}
		if json.Unmarshal(x.Detail, &d) == nil {
			for _, m := range d {
				add("missing:"+m.URL, "missing asset "+m.URL, journey, fmt.Sprintf("`%s` answered %d", m.URL, m.Status))
			}
			return
		}
	case "accessibility":
		var d []struct{ Rule, Target, Help string }
		if json.Unmarshal(x.Detail, &d) == nil {
			for _, v := range d {
				add("a11y:"+v.Rule, "new accessibility violation "+v.Rule, journey, fmt.Sprintf("%s at `%s` (%s)", v.Rule, oneLine(v.Target), oneLine(v.Help)))
			}
			return
		}
	case "markup validity":
		var d []struct{ Rule, Selector, Message string }
		if json.Unmarshal(x.Detail, &d) == nil {
			for _, v := range d {
				add("markup:"+v.Rule, "new markup error "+v.Rule, journey, fmt.Sprintf("%s at `%s`: %s", v.Rule, oneLine(v.Selector), oneLine(v.Message)))
			}
			return
		}
	case "JS errors":
		var d []string
		if json.Unmarshal(x.Detail, &d) == nil {
			for _, e := range d {
				msg := oneLine(e)
				if len(msg) > 120 {
					msg = msg[:120]
				}
				add("js:"+msg, "new JavaScript error", journey, "`"+strings.ReplaceAll(msg, "`", "'")+"`")
			}
			return
		}
	case "journey steps":
		var d []struct{ Error string }
		if json.Unmarshal(x.Detail, &d) == nil {
			for _, v := range d {
				add("steps:"+journey, "journey "+journey+" fails on the head build", journey, oneLine(v.Error))
			}
			return
		}
	case "DOM size":
		var d struct{ Base, Head int }
		if json.Unmarshal(x.Detail, &d) == nil {
			add("dom", "page DOM grew", journey, fmt.Sprintf("%d to %d elements", d.Base, d.Head))
			return
		}
	case "request count":
		var d struct{ Base, Head int }
		if json.Unmarshal(x.Detail, &d) == nil {
			add("requests", "more requests per page", journey, fmt.Sprintf("%d to %d requests", d.Base, d.Head))
			return
		}
	}
	if strings.HasSuffix(x.Metric, "bytes (gzip)") {
		var d struct {
			Base, Head int
			Culprits   []struct {
				Asset, Change string
				Delta         int
			}
		}
		if json.Unmarshal(x.Detail, &d) == nil {
			grown := false
			for _, c := range d.Culprits {
				if c.Delta <= 0 {
					continue
				}
				grown = true
				name := hashPart.ReplaceAllString(c.Asset, "$1")
				add("bytes:"+name, name+" grew", journey, fmt.Sprintf("`%s` %s, %+d gzip bytes (%s total %d to %d)", c.Asset, c.Change, c.Delta, x.Metric, d.Base, d.Head))
			}
			if grown {
				return
			}
			add("bytes:"+x.Metric, x.Metric+" grew", journey, fmt.Sprintf("%d to %d", d.Base, d.Head))
			return
		}
	}
	add("other:"+x.Metric, x.Metric+" regressed", journey, oneLine(string(x.Detail)))
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
