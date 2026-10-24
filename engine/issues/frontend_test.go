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
	got, err := FrontendIssues(feFixture(t), nil, 7, "dev")
	if err != nil {
		t.Fatal(err)
	}
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
	if got, _ := FrontendIssues(f, nil, 7, "dev"); got != nil {
		t.Fatalf("withheld run opened %d issues", len(got))
	}
}

func TestFrontendIssuesWithNotesCarryFixState(t *testing.T) {
	f := feFixture(t)
	f.JourneysUsed = json.RawMessage(`[{"id":"home","steps":[{"visit":"/"}]},{"id":"reports","steps":[{"visit":"/r"}]},{"id":"other","steps":[{"visit":"/o"}]}]`)
	f.ChangedFiles = []string{"web/src/App.jsx"}
	ids := FrontendIDs(f)
	if len(ids) != 2 || !strings.HasPrefix(ids[0], "fe-") {
		t.Fatalf("ids = %v", ids)
	}
	notes := model.Notes{Version: 1, Items: map[string]model.Note{
		ids[0]: {LikelyCause: "Likely the new chart library.", FixOptions: []string{"Load the chart library lazily."}},
	}}
	if err := ValidateFrontendNotes(notes, f); err != nil {
		t.Fatal(err)
	}
	got, err := FrontendIssues(f, &notes, 7, "dev")
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseMarker(got[0].Body)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != model.MarkerFrontend || m.FindingID != ids[0] || len(m.FixOptions) != 1 || m.Files[0] != "web/src/App.jsx" {
		t.Fatalf("marker = %+v", m)
	}
	// Only the journeys that showed this cause are carried.
	var js []struct{ ID string }
	if err := json.Unmarshal(m.FEJourneys, &js); err != nil || len(js) != 2 || js[0].ID != "home" || js[1].ID != "reports" {
		t.Fatalf("fe journeys = %s (%v)", m.FEJourneys, err)
	}
	if !strings.Contains(got[0].Body, "1. Load the chart library lazily.") || !strings.Contains(got[0].Body, "`fix it`") {
		t.Fatalf("body lacks the fix option or reply line:\n%s", got[0].Body)
	}
	if strings.Contains(got[1].Body, "`fix it` to apply") {
		t.Fatalf("an issue without notes offered fix it:\n%s", got[1].Body)
	}
}

func TestValidateFrontendNotesRejectsUnknownID(t *testing.T) {
	notes := model.Notes{Version: 1, Items: map[string]model.Note{"fe-00000000": {LikelyCause: "Likely x.", FixOptions: []string{"Do y."}}}}
	if err := ValidateFrontendNotes(notes, feFixture(t)); err == nil || !strings.Contains(err.Error(), "not a frontend finding ID") {
		t.Fatalf("err = %v", err)
	}
}
