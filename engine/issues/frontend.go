package issues

import (
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

// FrontendIssues opens one issue per root cause among the frontend
// regressions: the same culprit asset, missing URL, accessibility or markup
// rule, or JS error seen on several journeys is one issue. Withheld and
// improved checks open none.
func FrontendIssues(f model.Frontend, mr int, starter string) []Issue {
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
			if x.Verdict != "regression" {
				continue
			}
			feCauses(j.ID, x, add)
		}
	}
	keys := make([]string, 0, len(causes))
	for k := range causes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []Issue
	for _, k := range keys {
		c := causes[k]
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
		w("Not checked: " + strings.Join(f.NotChecked, ", ") + ".")
		w("")
		w("The `fix it` reply works on backend issues only; this one needs a human change.")
		out = append(out, Issue{FindingID: "fe:" + c.key, Title: "[Downstream] frontend: " + c.title, Body: b.String(), Steps: c.journeys})
	}
	return out
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
