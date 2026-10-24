package issues

import (
	"encoding/json"
	"strings"
	"testing"

	"downstream/engine/model"
)

func feFixture(t *testing.T) model.Frontend {
	t.Helper()
	var cmp model.FECompare
	err := json.Unmarshal([]byte(`{"journeys":[
	 {"id":"home","findings":[
	  {"metric":"js bytes (gzip)","verdict":"regression","detail":{"base":50000,"head":75000,"delta":25000,"pct":50,"culprits":[{"asset":"/assets/index-BBBBBBBB.js","change":"changed","base":50000,"head":75000,"delta":25000}]}},
	  {"metric":"missing assets","verdict":"regression","detail":[{"url":"/logo.png","status":404}]},
	  {"metric":"accessibility","verdict":"improved","detail":[{"rule":"image-alt","target":"img"}]}]},
	 {"id":"reports","findings":[
	  {"metric":"js bytes (gzip)","verdict":"regression","detail":{"base":60000,"head":85000,"delta":25000,"pct":41.7,"culprits":[{"asset":"/assets/index-BBBBBBBB.js","change":"changed","base":50000,"head":75000,"delta":25000}]}},
	  {"metric":"DOM size","verdict":"withheld","detail":{"base":30,"head":3000}}]}],
	 "noise":{"deterministic":true,"unstable":[]},"brokenOnBase":[]}`), &cmp)
	if err != nil {
		t.Fatal(err)
	}
	return model.Frontend{Status: model.FrontendChecked, Base: "aaaaaaaaaaaaaaaa", Head: "bbbbbbbbbbbbbbbb", Compare: &cmp, NotChecked: model.FENotChecked}
}

func TestFrontendIssuesOnePerRootCause(t *testing.T) {
	got := FrontendIssues(feFixture(t), 7, "dev")
	if len(got) != 2 {
		t.Fatalf("got %d issues, want 2 (shared bundle, missing logo)", len(got))
	}
	if got[0].Title != "[Downstream] frontend: /assets/index.js grew" || strings.Join(got[0].Steps, ",") != "home,reports" {
		t.Fatalf("bundle issue = %q on %v", got[0].Title, got[0].Steps)
	}
	if got[1].Title != "[Downstream] frontend: missing asset /logo.png" {
		t.Fatalf("missing issue = %q", got[1].Title)
	}
	if !strings.Contains(got[0].Body, "@dev") || !strings.Contains(got[0].Body, "!7") || !strings.Contains(got[0].Body, "color-contrast") {
		t.Fatalf("body lacks the starter, MR or not-checked line:\n%s", got[0].Body)
	}
}

func TestFrontendIssuesNoneUnlessChecked(t *testing.T) {
	f := feFixture(t)
	f.Status = model.FrontendWithheld
	if got := FrontendIssues(f, 7, "dev"); got != nil {
		t.Fatalf("withheld run opened %d issues", len(got))
	}
}
