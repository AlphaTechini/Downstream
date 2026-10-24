package report

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"downstream/engine/model"
)

const maxDetailItems = 3

// RenderFrontend returns the Frontend section of the note. Every figure comes
// from frontend.json as fecheck wrote it.
func RenderFrontend(f model.Frontend) string {
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }
	line("#### Frontend")
	line("")
	switch f.Status {
	case model.FrontendSkipped:
		line("**Frontend:** skipped, " + f.Reason + ".")
	case model.FrontendWithheld:
		msg := oneLine(f.FailedMessage)
		if len(msg) > 300 {
			msg = msg[:300] + "..."
		}
		line(fmt.Sprintf("**Frontend:** withheld. The %s stage failed: %s. No frontend result is claimed.", f.FailedStage, code(msg)))
	case model.FrontendChecked:
		frontendChecked(&b, f)
	default:
		line("**Frontend:** no result.")
	}
	line("")
	line("Not checked: " + strings.Join(f.NotChecked, ", ") + ". Pages are rendered in happy-dom, which runs the app's JavaScript but has no layout or paint.")
	return b.String()
}

func frontendChecked(b *strings.Builder, f model.Frontend) {
	line := func(s string) { b.WriteString(s + "\n") }
	counts := map[string]int{}
	var rows []string
	for _, j := range f.Compare.Journeys {
		for _, x := range j.Findings {
			if x.Metric == "not checked" {
				continue
			}
			counts[x.Verdict]++
			rows = append(rows, fmt.Sprintf("| %s | %s | %s | %s |", cell(j.ID), cell(x.Metric), x.Verdict, cell(feDetail(x))))
		}
	}
	line(fmt.Sprintf("**Frontend (%s build, %d journeys, Node %s, base %s, head %s):** regressions %d, improved %d, withheld %d.",
		f.Mode, f.Journeys, f.NodeVersion, short(f.Base), short(f.Head), counts["regression"], counts["improved"], counts["withheld"]))
	line("")
	if len(rows) == 0 {
		line("No page check moved between base and head.")
	} else {
		line("| Journey | Check | Verdict | Detail |")
		line("|---|---|---|---|")
		for _, r := range rows {
			line(r)
		}
	}
	line("")
	if n := f.Compare.Noise; n != nil {
		if n.Deterministic {
			line("Base against base: every page check gave the same result twice, so the changes above are not noise.")
		} else {
			var ids []string
			for _, u := range n.Unstable {
				ids = append(ids, u.ID)
			}
			line("Base against base: these journeys moved between two base runs, so their moved checks are withheld: " + cell(strings.Join(ids, ", ")) + ".")
		}
	}
	for _, br := range f.Compare.BrokenOnBase {
		msg := ""
		if len(br.Failures) > 0 {
			msg = ": " + code(br.Failures[0].Error)
		}
		line("Not compared, already failing on the base build: " + cell(br.ID) + msg + ".")
	}
	if len(f.Dropped) > 0 {
		line("")
		line("Journey steps the validator refused:")
		for _, d := range f.Dropped {
			line("- " + cell(d))
		}
	}
	if len(f.ChangedFiles) > 0 {
		line("")
		files := f.ChangedFiles
		more := ""
		if len(files) > 8 {
			more = fmt.Sprintf(" and %d more", len(files)-8)
			files = files[:8]
		}
		var cs []string
		for _, p := range files {
			cs = append(cs, code(p))
		}
		line("Frontend files changed: " + strings.Join(cs, ", ") + more + ".")
	}
}

// feDetail turns fecheck's per-metric detail into one cell of text.
func feDetail(x model.FEFinding) string {
	switch x.Metric {
	case "request count":
		var d struct{ Base, Head int }
		if json.Unmarshal(x.Detail, &d) == nil {
			return fmt.Sprintf("%d to %d requests", d.Base, d.Head)
		}
	case "DOM size":
		var d struct {
			Base, Head int
			Growth     [][2]any `json:"growth"`
		}
		if json.Unmarshal(x.Detail, &d) == nil {
			s := fmt.Sprintf("%d to %d elements", d.Base, d.Head)
			var g []string
			for _, t := range d.Growth {
				g = append(g, fmt.Sprintf("+%v %v", t[1], t[0]))
			}
			if len(g) > 0 {
				s += " (" + strings.Join(limit(g), ", ") + ")"
			}
			return s
		}
	case "missing assets":
		var d []struct {
			URL    string `json:"url"`
			Status int    `json:"status"`
		}
		if json.Unmarshal(x.Detail, &d) == nil {
			var s []string
			for _, m := range d {
				s = append(s, fmt.Sprintf("%s (%d)", m.URL, m.Status))
			}
			return strings.Join(limit(s), ", ")
		}
	case "accessibility":
		var d []struct{ Rule, Target string }
		if json.Unmarshal(x.Detail, &d) == nil {
			var s []string
			for _, v := range d {
				s = append(s, v.Rule+" at "+v.Target)
			}
			return strings.Join(limit(s), "; ")
		}
	case "markup validity":
		var d []struct{ Rule, Selector string }
		if json.Unmarshal(x.Detail, &d) == nil {
			var s []string
			for _, v := range d {
				s = append(s, v.Rule+" at "+v.Selector)
			}
			return strings.Join(limit(s), "; ")
		}
	case "JS errors":
		var d []string
		if json.Unmarshal(x.Detail, &d) == nil {
			return strings.Join(limit(d), "; ")
		}
	case "journey steps":
		var d []struct{ Error string }
		if json.Unmarshal(x.Detail, &d) == nil {
			var s []string
			for _, v := range d {
				s = append(s, v.Error)
			}
			return strings.Join(limit(s), "; ")
		}
	}
	if strings.HasSuffix(x.Metric, "bytes (gzip)") {
		var d struct {
			Base, Head, Delta int
			Pct               float64
			Culprits          []struct {
				Asset, Change string
				Delta         int
			}
		}
		if json.Unmarshal(x.Detail, &d) == nil {
			s := fmt.Sprintf("%s to %s gzip bytes (%s%%)", strconv.Itoa(d.Base), strconv.Itoa(d.Head), signed(d.Pct, 1))
			var c []string
			for _, a := range d.Culprits {
				c = append(c, fmt.Sprintf("%s %s %+d", a.Asset, a.Change, a.Delta))
			}
			if len(c) > 0 {
				s += "; " + strings.Join(limit(c), ", ")
			}
			return s
		}
	}
	return oneLine(string(x.Detail))
}

// limit keeps the first few items and says how many were left out.
func limit(s []string) []string {
	if len(s) <= maxDetailItems {
		return s
	}
	return append(s[:maxDetailItems:maxDetailItems], fmt.Sprintf("%d more", len(s)-maxDetailItems))
}
