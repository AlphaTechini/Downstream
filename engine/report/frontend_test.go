package report

import (
	"encoding/json"
	"strings"
	"testing"

	"downstream/engine/model"
)

func TestRenderFrontendChecked(t *testing.T) {
	var cmp model.FECompare
	if err := json.Unmarshal([]byte(`{"journeys":[{"id":"home","findings":[
	  {"metric":"js bytes (gzip)","verdict":"regression","detail":{"base":50000,"head":75000,"delta":25000,"pct":50,"culprits":[{"asset":"/assets/index-B.js","change":"changed","delta":25000}]}},
	  {"metric":"not checked","verdict":"withheld","detail":["color-contrast"]},
	  {"metric":"accessibility","verdict":"regression","detail":[{"rule":"image-alt","target":"img.logo"}]}]}],
	  "noise":{"deterministic":true,"unstable":[]},"brokenOnBase":[{"id":"cart","failures":[{"error":"click target not found: #buy"}]}]}`), &cmp); err != nil {
		t.Fatal(err)
	}
	out := RenderFrontend(model.Frontend{
		Status: model.FrontendChecked, Mode: "static", Journeys: 2, NodeVersion: "20.20.2",
		Base: "aaaaaaaaaaaaaaaa", Head: "bbbbbbbbbbbbbbbb", Compare: &cmp, NotChecked: model.FENotChecked,
		ChangedFiles: []string{"web/src/App.tsx"}, Dropped: []string{"journey x step 2: selector does not parse"},
	})
	for _, want := range []string{
		"#### Frontend",
		"regressions 2, improved 0, withheld 0",
		"| home | js bytes (gzip) | regression | 50000 to 75000 gzip bytes (+50.0%); /assets/index-B.js changed +25000 |",
		"| home | accessibility | regression | image-alt at img.logo |",
		"Base against base: every page check gave the same result twice",
		"Not compared, already failing on the base build: cart: `click target not found: #buy`.",
		"- journey x step 2: selector does not parse",
		"Not checked: color-contrast, visual layout, real paint/CPU timing.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "| not checked |") {
		t.Errorf("the not-checked marker became a table row:\n%s", out)
	}
}

func TestRenderFrontendWithheldAndSkipped(t *testing.T) {
	w := RenderFrontend(model.Frontend{Status: model.FrontendWithheld, FailedStage: "frontend build (head)", FailedMessage: "npm run build failed", NotChecked: model.FENotChecked})
	if !strings.Contains(w, "withheld. The frontend build (head) stage failed: `npm run build failed`. No frontend result is claimed.") {
		t.Errorf("withheld:\n%s", w)
	}
	s := RenderFrontend(model.Frontend{Status: model.FrontendSkipped, Reason: "the change touches no frontend file", NotChecked: model.FENotChecked})
	if !strings.Contains(s, "skipped, the change touches no frontend file.") || !strings.Contains(s, "Not checked:") {
		t.Errorf("skipped:\n%s", s)
	}
}
