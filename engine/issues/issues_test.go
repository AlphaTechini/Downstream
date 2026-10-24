package issues

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"downstream/engine/model"
)

const (
	baseSHA = "1111111122223333444455556666777788889999"
	headSHA = "aaaaaaaabbbbccccddddeeeeffff000011112222"
)

func journey() model.Journey {
	return model.Journey{
		ID: "shelf", Goal: "Reader adds a book to their shelf", WhyIncluded: "calls the changed route",
		Steps: []model.Step{
			{Name: "signup", Method: "POST", Path: "/users", Body: json.RawMessage(`{"name":"u{{uniq}}"}`), ExpectStatus: 201},
			{Name: "add_book", Method: "POST", Path: "/shelf/books", ExpectStatus: 201,
				Headers: map[string]string{"X-Note": "a -- b"}},
			{Name: "open_book", Method: "GET", Path: "/books/{{book_id}}?full=1", ExpectStatus: 200},
		},
	}
}

func fixture() model.Run {
	return model.Run{
		Version: 1, Base: baseSHA, Head: headSHA,
		Journeys: []model.Journey{journey(), {ID: "catalog", Goal: "Reader searches", Steps: []model.Step{{Name: "search", Method: "GET", Path: "/search", ExpectStatus: 200}}}},
		Findings: []model.Finding{
			{ID: "shelf.add_book.p95_ms", Journey: "shelf", Step: "add_book", Metric: model.MetricP95, Unit: "ms",
				B1: 46.3, C: 458.6, B2: 48.1, Band: 1.8, Delta: 410.5, DeltaPct: 853.4, Class: model.ClassRegression,
				Reason: "worse than the worse baseline by 410.5 ms"},
			{ID: "shelf.add_book.p50_ms", Journey: "shelf", Step: "add_book", Metric: model.MetricP50, Unit: "ms",
				B1: 12, C: 120, B2: 12.5, Band: 0.5, Delta: 107.5, DeltaPct: 860, Class: model.ClassRegression,
				Reason: "worse than the worse baseline by 107.5 ms"},
			{ID: "shelf.signup.p95_ms", Journey: "shelf", Step: "signup", Metric: model.MetricP95, Unit: "ms",
				B1: 14, C: 14.5, B2: 14.2, Band: 0.2, Delta: 0.3, DeltaPct: 2, Class: model.ClassNoise},
			{ID: "shelf.signup.failures", Journey: "shelf", Step: "signup", Metric: model.MetricFailures, Unit: "count",
				B1: 0, C: 30, B2: 0, Band: 0, Delta: 30, Class: model.ClassRegression,
				Reason: "passed on both baseline runs, failed 30 times on the candidate"},
			{ID: "app.cpu_seconds", Metric: model.MetricCPU, Unit: "s",
				B1: 2.31, C: 4.87, B2: 2.35, Band: 0.04, Delta: 2.52, DeltaPct: 107.2, Class: model.ClassRegression},
			{ID: "catalog.search.p95_ms", Journey: "catalog", Step: "search", Metric: model.MetricP95, Unit: "ms",
				B1: 120, C: 60, B2: 123, Band: 3, Delta: -63, DeltaPct: -51, Class: model.ClassTradeOff},
			{ID: "catalog.search.p50_ms", Journey: "catalog", Step: "search", Metric: model.MetricP50, Unit: "ms",
				B1: 20, C: 80, B2: 21, Band: 1, Delta: 59, DeltaPct: 280, Class: model.ClassTradeOff},
		},
	}
}

func scanFixture() *model.Scan {
	return &model.Scan{
		Changed: []model.Impact{{
			Route:        model.Route{Method: "POST", Path: "/shelf/books"},
			Reasons:      []string{"handler calls store.AddBook"},
			ChangedFuncs: []string{"store.AddBook"},
		}},
		Affected: []model.Impact{
			{Route: model.Route{Method: "GET", Path: "/books/{id}"}, ChangedFuncs: []string{"store.Lookup"}},
			{Route: model.Route{Method: "GET", Path: "/other"}, ChangedFuncs: []string{"store.Unrelated"}},
		},
	}
}

func notesFixture() *model.Notes {
	return &model.Notes{Version: 1, Items: map[string]model.Note{
		"shelf.add_book.p95_ms": {
			LikelyCause: "AddBook now scans every shelf row before inserting.",
			FixOptions:  []string{"Look the book up by key instead of scanning.", "Revert the scan."},
		},
		"shelf.signup.failures": {LikelyCause: "Likely a new validation rejects names with digits.", FixOptions: []string{"Relax the check."}},
	}}
}

func TestBuild(t *testing.T) {
	is, err := Build(fixture(), scanFixture(), notesFixture(), 12, "@alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(is) != 2 {
		t.Fatalf("got %d issues, want 2 (one per regressed step; the app regression is listed inside them)", len(is))
	}
	byID := map[string]Issue{}
	for _, i := range is {
		byID[i.FindingID] = i
	}

	p95 := byID["shelf.add_book.p95_ms"]
	wantTitle := "[Downstream] Reader adds a book to their shelf: p95 latency up 410.5 ms (+853%)"
	if p95.Title != wantTitle {
		t.Errorf("title = %q, want %q", p95.Title, wantTitle)
	}
	if got := byID["shelf.signup.failures"].Title; got != "[Downstream] Reader adds a book to their shelf: failures up 30 (was 0)" {
		t.Errorf("failures title = %q", got)
	}
	if _, ok := byID["shelf.add_book.p50_ms"]; ok {
		t.Errorf("a non-headline metric got its own issue")
	}
	if _, ok := byID["app.cpu_seconds"]; ok {
		t.Errorf("app regression got its own issue although steps regressed")
	}

	body := p95.Body
	order := []string{
		"Run started by @alice on !12.",
		"**Changed functions**",
		"- `store.AddBook` on `POST /shelf/books`",
		"**Measured**",
		"| Journey | Step | Metric | B1 | B2 | C | Noise band | Change |",
		"| shelf | add_book | p95 latency | 46.3 ms | 48.1 ms | 458.6 ms | 1.8 ms | +410.5 ms (+853%) |",
		"App process:",
		"- CPU time: B1 2.31 s, B2 2.35 s, C 4.87 s, noise band 0.04 s, change +2.52 s (+107%) against the worse baseline",
		"**Repro**",
		"1. `POST /users`, expect 201",
		"3. `GET /books/{{book_id}}?full=1`, expect 200",
		"downstream run --base " + baseSHA + " --head " + headSHA + " --journeys journeys.json --out run.json",
		"downstream marker parse --journeys-out journeys.json < issue.md",
		"**Likely cause**",
		"Likely: AddBook now scans every shelf row before inserting.",
		"**Fix options**",
		"1. Look the book up by key instead of scanning.",
		"2. Revert the scan.",
		"**How to reply**",
		"Reply with a mention of Downstream followed by `fix it` to apply option 1, or `fix option N`",
		"downstream:v1 ",
	}
	pos := 0
	for _, w := range order {
		i := strings.Index(body[pos:], w)
		if i < 0 {
			t.Fatalf("%q missing or out of order in body:\n%s", w, body)
		}
		pos += i + len(w)
	}
	if strings.Contains(body, "store.Lookup") || strings.Contains(body, "store.Unrelated") {
		t.Errorf("function from a route outside the group is listed")
	}
	if !reflect.DeepEqual(p95.Steps, []string{"shelf.add_book.p95_ms"}) {
		t.Errorf("steps = %v", p95.Steps)
	}
	if last := lastLine(body); last != "</details>" {
		t.Errorf("body does not end with the state section: %q", last)
	}
	m := markerLine(t, body)
	if strings.Contains(strings.TrimSuffix(strings.TrimPrefix(m, markerPrefix), markerSuffix), "--") {
		t.Errorf("marker JSON contains \"--\": %s", m)
	}
}

func markerLine(t *testing.T, body string) string {
	t.Helper()
	l := ""
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, markerPrefix) {
			l = line
		}
	}
	if l == "" {
		t.Fatalf("no marker line in body")
	}
	return l
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	return s[strings.LastIndex(s, "\n")+1:]
}

func TestMarkerRoundTrip(t *testing.T) {
	is, err := Build(fixture(), scanFixture(), notesFixture(), 12, "alice")
	if err != nil {
		t.Fatal(err)
	}
	var issue Issue
	for _, i := range is {
		if i.FindingID == "shelf.add_book.p95_ms" {
			issue = i
		}
	}
	m, err := ParseMarker(issue.Body)
	if err != nil {
		t.Fatal(err)
	}
	want := model.Marker{
		Version: 1, MR: 12, Base: baseSHA, Head: headSHA, FindingID: "shelf.add_book.p95_ms",
		Metric: model.MetricP95, Step: "add_book", Journey: journey(),
		B1: 46.3, C: 458.6, B2: 48.1, Band: 1.8,
		FixOptions: notesFixture().Items["shelf.add_book.p95_ms"].FixOptions,
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("marker round trip differs:\n got %+v\nwant %+v", m, want)
	}
	if got := m.Journey.Steps[1].Headers["X-Note"]; got != "a -- b" {
		t.Errorf("header with -- came back as %q", got)
	}
	if strings.Contains(strings.TrimSuffix(markerLine(t, issue.Body)[len(markerPrefix):], markerSuffix), "--") {
		t.Errorf("marker JSON still has --")
	}
}

func TestMarkerTricky(t *testing.T) {
	m := model.Marker{Version: 1, MR: 3, Base: baseSHA, Head: headSHA, FindingID: "a.b.c",
		FixOptions: []string{"x --> y <!-- z ---- \\-- \"q\" > < ``` `"}}
	line, err := encodeMarker(m)
	if err != nil {
		t.Fatal(err)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(line, markerPrefix), markerSuffix)
	if strings.ContainsAny(inner, "<>`") || strings.Contains(inner, "--") {
		t.Errorf("unsafe characters in marker JSON: %s", inner)
	}
	got, err := ParseMarker("text\r\n" + line + "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.FixOptions, m.FixOptions) {
		t.Errorf("got %q want %q", got.FixOptions, m.FixOptions)
	}
}

func TestParseMarkerErrors(t *testing.T) {
	good, _ := encodeMarker(model.Marker{Version: 1, MR: 3, Base: baseSHA, Head: headSHA, FindingID: "a.b.c"})
	cases := map[string]string{
		"none":        "just text",
		"unclosed":    legacyPrefix + strings.TrimPrefix(good, markerPrefix),
		"bad json":    markerPrefix + "{nope" + markerSuffix,
		"bad version": markerPrefix + `{"v":2,"mr":1,"base":"` + baseSHA + `","head":"` + headSHA + `","finding":"a"}` + markerSuffix,
		"bad sha":     markerPrefix + `{"v":1,"mr":1,"base":"main; rm -rf /","head":"` + headSHA + `","finding":"a"}` + markerSuffix,
		"no mr":       markerPrefix + `{"v":1,"mr":0,"base":"` + baseSHA + `","head":"` + headSHA + `","finding":"a"}` + markerSuffix,
		"no finding":  markerPrefix + `{"v":1,"mr":1,"base":"` + baseSHA + `","head":"` + headSHA + `"}` + markerSuffix,
		"trailing":    markerPrefix + `{"v":1,"mr":1,"base":"` + baseSHA + `","head":"` + headSHA + `","finding":"a"} {}` + markerSuffix,
	}
	for name, body := range cases {
		if _, err := ParseMarker(body); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := ParseMarker(good); err != nil {
		t.Errorf("good marker rejected: %v", err)
	}
	if _, err := ParseMarker(legacyPrefix + strings.TrimPrefix(good, markerPrefix) + legacySuffix); err != nil {
		t.Errorf("legacy comment marker rejected: %v", err)
	}
}

func TestFixCheck(t *testing.T) {
	m := model.Marker{FixOptions: []string{"first", "second"}}
	for n, want := range map[int]string{1: "first", 2: "second"} {
		got, err := FixCheck(m, n)
		if err != nil || got != want {
			t.Errorf("FixCheck(%d) = %q, %v", n, got, err)
		}
	}
	for _, n := range []int{0, 3, -1} {
		if _, err := FixCheck(m, n); err == nil {
			t.Errorf("FixCheck(%d) should fail", n)
		}
	}
	if _, err := FixCheck(model.Marker{}, 1); err == nil || !strings.Contains(err.Error(), "no fix options") {
		t.Errorf("empty options: %v", err)
	}
}

func TestValidateNotes(t *testing.T) {
	run := fixture()
	ok := model.Note{LikelyCause: "Scans every row.", FixOptions: []string{"Use a key lookup."}}
	notes := func(id string, n model.Note) model.Notes {
		return model.Notes{Version: 1, Items: map[string]model.Note{id: n}}
	}
	if err := ValidateNotes(notes("shelf.add_book.p95_ms", ok), run, scanFixture()); err != nil {
		t.Fatalf("good notes rejected: %v", err)
	}
	long := func(n int) string { return strings.Repeat("a", n) }
	cases := []struct {
		name string
		id   string
		n    model.Note
		want string
	}{
		{"unknown id", "nope.x.p95_ms", ok, "not a finding ID"},
		{"not a regression", "shelf.signup.p95_ms", ok, "not a regression"},
		{"not the headline", "shelf.add_book.p50_ms", ok, "not a headline finding"},
		{"app finding while steps regressed", "app.cpu_seconds", ok, "not a headline finding"},
		{"empty cause", "shelf.add_book.p95_ms", model.Note{FixOptions: ok.FixOptions}, "likely_cause must be 1 to 600"},
		{"long cause", "shelf.add_book.p95_ms", model.Note{LikelyCause: long(601), FixOptions: ok.FixOptions}, "likely_cause must be 1 to 600"},
		{"no options", "shelf.add_book.p95_ms", model.Note{LikelyCause: "x"}, "1 to 5 entries"},
		{"six options", "shelf.add_book.p95_ms", model.Note{LikelyCause: "x", FixOptions: []string{"a", "b", "c", "d", "e", "f"}}, "1 to 5 entries"},
		{"empty option", "shelf.add_book.p95_ms", model.Note{LikelyCause: "x", FixOptions: []string{" "}}, "1 to 400"},
		{"long option", "shelf.add_book.p95_ms", model.Note{LikelyCause: "x", FixOptions: []string{long(401)}}, "1 to 400"},
		{"comment open", "shelf.add_book.p95_ms", model.Note{LikelyCause: "a <!-- b", FixOptions: ok.FixOptions}, "<!--"},
		{"comment close", "shelf.add_book.p95_ms", model.Note{LikelyCause: "a --> b", FixOptions: ok.FixOptions}, "-->"},
		{"fence", "shelf.add_book.p95_ms", model.Note{LikelyCause: "x", FixOptions: []string{"```sh\nrm\n```"}}, "```"},
		{"table", "shelf.add_book.p95_ms", model.Note{LikelyCause: "x\n| a | b |", FixOptions: ok.FixOptions}, "starting with"},
		{"ms figure", "shelf.add_book.p95_ms", model.Note{LikelyCause: "It takes 412.3 ms now.", FixOptions: ok.FixOptions}, "412.3 ms"},
		{"percent", "shelf.add_book.p95_ms", model.Note{LikelyCause: "x", FixOptions: []string{"Saves 40% of the time."}}, "40%"},
		{"MB figure", "shelf.add_book.p95_ms", model.Note{LikelyCause: "Uses 12MB more.", FixOptions: ok.FixOptions}, "12MB"},
		{"seconds", "shelf.add_book.p95_ms", model.Note{LikelyCause: "Waits 3 s.", FixOptions: ok.FixOptions}, "3 s"},
	}
	for _, tc := range cases {
		err := ValidateNotes(notes(tc.id, tc.n), run, scanFixture())
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want containing %q", tc.name, err, tc.want)
		}
	}
	// words that merely start with a unit letter, or identifiers with digits, are fine
	fine := model.Note{LikelyCause: "Calls step 2 of the p95 path and 3 steps of setup.", FixOptions: []string{"Cache results for 2 seconds."}}
	if err := ValidateNotes(notes("shelf.add_book.p95_ms", fine), run, scanFixture()); err != nil {
		t.Errorf("harmless text rejected: %v", err)
	}
}

func TestBuildRejects(t *testing.T) {
	for _, st := range []string{"", "a b", "a;b", "@@x", "x/y", strings.Repeat("a", 256)} {
		if _, err := Build(fixture(), nil, nil, 1, st); err == nil {
			t.Errorf("starter %q accepted", st)
		}
	}
	if _, err := Build(fixture(), nil, nil, 0, "a"); err == nil {
		t.Errorf("mr 0 accepted")
	}
	bad := notesFixture()
	bad.Items["shelf.add_book.p95_ms"] = model.Note{LikelyCause: "takes 5 ms", FixOptions: []string{"x"}}
	if _, err := Build(fixture(), nil, bad, 1, "a"); err == nil {
		t.Errorf("invalid notes accepted")
	}
	run := fixture()
	run.Base = "main"
	if _, err := Build(run, nil, nil, 1, "a"); err == nil {
		t.Errorf("non-hex base accepted")
	}
	run = fixture()
	run.Journeys = nil
	if _, err := Build(run, nil, nil, 1, "a"); err == nil {
		t.Errorf("finding without journey accepted")
	}
}

func TestBuildWithoutNotesOrScan(t *testing.T) {
	is, err := Build(fixture(), nil, nil, 7, "bob")
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range is {
		for _, w := range []string{
			"Not written: no notes were supplied for this finding.",
			"None supplied.",
			"Not available: no scan was supplied.",
			"nothing to apply",
		} {
			if !strings.Contains(i.Body, w) {
				t.Errorf("%s: missing %q", i.FindingID, w)
			}
		}
		m, err := ParseMarker(i.Body)
		if err != nil {
			t.Fatal(err)
		}
		if m.FixOptions == nil || len(m.FixOptions) != 0 {
			t.Errorf("fix options should be an empty list, got %#v", m.FixOptions)
		}
	}
}

func TestLikelyPrefix(t *testing.T) {
	n := notesFixture()
	is, err := Build(fixture(), scanFixture(), n, 1, "a")
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range is {
		switch i.FindingID {
		case "shelf.signup.failures":
			if !strings.Contains(i.Body, "\nLikely a new validation") || strings.Contains(i.Body, "Likely: Likely") {
				t.Errorf("prefix added twice:\n%s", i.Body)
			}
		case "shelf.add_book.p95_ms":
			if !strings.Contains(i.Body, "\nLikely: AddBook") {
				t.Errorf("prefix missing")
			}
		}
	}
}

func TestQuestions(t *testing.T) {
	qs := Questions(fixture())
	if len(qs) != 1 {
		t.Fatalf("got %d questions, want 1", len(qs))
	}
	q := qs[0]
	for _, w := range []string{
		"**Question for the reviewers:** on Reader searches",
		"No issue opened.",
		"Better (change against the worse baseline run):\n\n- `search` p95 latency: -63.0 ms (-51%)",
		"Worse (change against the worse baseline run):\n\n- `search` p50 latency: +59.0 ms (+280%)",
	} {
		if !strings.Contains(q, w) {
			t.Errorf("missing %q in:\n%s", w, q)
		}
	}
	if got := Questions(model.Run{}); len(got) != 0 {
		t.Errorf("no trade-offs should give no questions")
	}
}

func TestRouteMatches(t *testing.T) {
	cases := []struct {
		pm, pat, m, path string
		want             bool
	}{
		{"GET", "/books/{id}", "GET", "/books/42", true},
		{"GET", "/books/{id}", "GET", "/books/{{book_id}}?full=1", true},
		{"GET", "/books/{id}", "POST", "/books/42", false},
		{"", "/books/{id}", "POST", "/books/42", true},
		{"GET", "/books/{id}", "GET", "/books", false},
		{"GET", "/books/{id}", "GET", "/books/1/reviews", false},
		{"GET", "/books/{id}/reviews", "GET", "/books/1/reviews", true},
		{"GET", "/files/{p...}", "GET", "/files/a/b/c", true},
		{"GET", "/files/{p...}", "GET", "/files", false},
		{"GET", "/static/", "GET", "/static/css/a.css", true},
		{"GET", "/{$}", "GET", "/", true},
		{"GET", "/{$}", "GET", "/x", false},
		{"GET", "/health", "GET", "/health", true},
		{"GET", "/health", "GET", "/healthz", false},
		{"GET", "/books/{id}", "GET", "/books/", false},
	}
	for _, tc := range cases {
		if got := routeMatches(tc.pm, tc.pat, tc.m, tc.path); got != tc.want {
			t.Errorf("routeMatches(%q %q, %q %q) = %v, want %v", tc.pm, tc.pat, tc.m, tc.path, got, tc.want)
		}
	}
}

// groupFixture has three causes: a function unique to one step, a shared
// function carried by two steps, and public routes with no changed function.
func groupFixture() (model.Run, *model.Scan) {
	step := func(name, method, path string) model.Step {
		return model.Step{Name: name, Method: method, Path: path, ExpectStatus: 200}
	}
	run := model.Run{Version: 1, Base: baseSHA, Head: headSHA, Journeys: []model.Journey{
		{ID: "j1", Goal: "Writer edits", Steps: []model.Step{step("s1", "POST", "/a"), step("s2", "GET", "/b"), step("s3", "GET", "/b?x=1")}},
		{ID: "j2", Goal: "Visitor browses", Steps: []model.Step{step("p1", "GET", "/pub"), step("p2", "GET", "/pub/1")}},
	}}
	reg := func(j, s, metric string, delta, pct float64) model.Finding {
		return model.Finding{ID: j + "." + s + "." + metric, Journey: j, Step: s, Metric: metric, Unit: "ms",
			B1: 100, B2: 100, C: 100 + delta, Delta: delta, DeltaPct: pct, Class: model.ClassRegression, Reason: "rule"}
	}
	run.Findings = []model.Finding{
		reg("j1", "s1", model.MetricP95, 50, 50),
		reg("j1", "s2", model.MetricP95, 80, 80),
		reg("j1", "s3", model.MetricP50, 120, 120),
		reg("j2", "p1", model.MetricP95, 30, 30),
		reg("j2", "p2", model.MetricP95, 200, 200),
		{ID: "app.peak_rss_mb", Metric: model.MetricRSS, Unit: "MB", B1: 50, B2: 50, C: 90, Delta: 40, DeltaPct: 80, Class: model.ClassRegression, Reason: "rss"},
	}
	scan := &model.Scan{Changed: []model.Impact{
		{Route: model.Route{Method: "POST", Path: "/a"}, ChangedFuncs: []string{"pkg.Shared", "pkg.OnlyA"}},
		{Route: model.Route{Method: "GET", Path: "/b"}, ChangedFuncs: []string{"pkg.Shared"}},
	}}
	return run, scan
}

func TestGrouping(t *testing.T) {
	run, scan := groupFixture()
	plan := PlanIssues(run, scan)
	if plan.RegressedSteps != 5 || plan.TotalGroups != 3 || plan.LeftOutGroups != 0 {
		t.Fatalf("plan counts: %+v", plan)
	}
	type g struct {
		key   string
		steps []string
		head  string
	}
	var got []g
	for _, gr := range plan.Groups {
		var ids []string
		for _, s := range gr.Steps {
			ids = append(ids, s.ID)
		}
		got = append(got, g{strings.Join(gr.Key, ","), ids, gr.Headline.ID})
	}
	want := []g{
		{"pkg.Shared", []string{"j1.s2.p95_ms", "j1.s3.p50_ms"}, "j1.s3.p50_ms"},
		{"", []string{"j2.p1.p95_ms", "j2.p2.p95_ms"}, "j2.p2.p95_ms"},
		{"pkg.OnlyA,pkg.Shared", []string{"j1.s1.p95_ms"}, "j1.s1.p95_ms"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("groups:\n got %+v\nwant %+v", got, want)
	}
	// app regression joins the first of the largest groups
	if len(plan.Groups[0].App) != 1 || len(plan.Groups[1].App) != 0 {
		t.Errorf("app regression not attached to the largest group")
	}

	is, err := Build(run, scan, nil, 4, "dana")
	if err != nil {
		t.Fatal(err)
	}
	titles := []string{
		"[Downstream] 2 steps slower via pkg.Shared: worst j1.s3 p50 latency up 120.0 ms (+120%)",
		"[Downstream] 2 steps slower on routes outside the diff's call paths: worst j2.p2 p95 latency up 200.0 ms (+200%)",
		"[Downstream] Writer edits: p95 latency up 50.0 ms (+50%)",
	}
	for i, is := range is {
		if is.Title != titles[i] {
			t.Errorf("title %d = %q, want %q", i, is.Title, titles[i])
		}
	}
	if !reflect.DeepEqual(is[0].Steps, []string{"j1.s2.p95_ms", "j1.s3.p50_ms"}) {
		t.Errorf("steps = %v", is[0].Steps)
	}
	if !strings.Contains(is[1].Body, "None: no changed function") || strings.Contains(is[1].Body, "peak memory") {
		t.Errorf("outside body:\n%s", is[1].Body)
	}
	if !strings.Contains(is[0].Body, "peak memory: B1 50.0 MB") {
		t.Errorf("app process missing from the largest group:\n%s", is[0].Body)
	}
	if !strings.Contains(is[0].Body, "- `pkg.Shared` on `GET /b`") || strings.Contains(is[0].Body, "pkg.OnlyA") {
		t.Errorf("shared body changed functions wrong:\n%s", is[0].Body)
	}
	if !strings.Contains(is[0].Body, "| j1 | s2 | p95 latency |") || !strings.Contains(is[0].Body, "| j1 | s3 | p50 latency |") {
		t.Errorf("table rows missing:\n%s", is[0].Body)
	}
	if !strings.Contains(is[2].Body, "- `pkg.OnlyA` on `POST /a`") || !strings.Contains(is[2].Body, "- `pkg.Shared` on `POST /a`") {
		t.Errorf("third body changed functions wrong:\n%s", is[2].Body)
	}
	// notes are keyed by group headline only
	notes := model.Notes{Version: 1, Items: map[string]model.Note{"j2.p1.p95_ms": {LikelyCause: "x", FixOptions: []string{"y"}}}}
	if _, err := Build(run, scan, &notes, 4, "dana"); err == nil || !strings.Contains(err.Error(), "not a headline") {
		t.Errorf("non-headline note accepted: %v", err)
	}
}

func TestGroupingWithoutScan(t *testing.T) {
	run, _ := groupFixture()
	plan := PlanIssues(run, nil)
	if len(plan.Groups) != 1 || len(plan.Groups[0].Steps) != 5 {
		t.Fatalf("want one group of 5 steps, got %+v", plan)
	}
	is, err := Build(run, nil, nil, 4, "dana")
	if err != nil || len(is) != 1 {
		t.Fatalf("%v %d", err, len(is))
	}
	if is[0].Title != "[Downstream] 5 steps slower: worst j2.p2 p95 latency up 200.0 ms (+200%)" {
		t.Errorf("title = %q", is[0].Title)
	}
	if !strings.Contains(is[0].Body, "Not available: no scan was supplied.") {
		t.Errorf("body should say no scan")
	}
}

func TestFailuresHeadlineFirst(t *testing.T) {
	run, _ := groupFixture()
	run.Findings = append(run.Findings, model.Finding{ID: "j1.s1.failures", Journey: "j1", Step: "s1", Metric: model.MetricFailures,
		Unit: "count", C: 3, Delta: 3, Class: model.ClassRegression})
	h := Headlines(run, nil)
	if len(h) != 1 || h[0].ID != "j1.s1.failures" {
		t.Errorf("headlines = %+v, want the failures finding", h)
	}
}

func TestIssueCap(t *testing.T) {
	// seven causes, one function each, with 1 to 7 steps
	var run model.Run
	run.Base, run.Head = baseSHA, headSHA
	scan := &model.Scan{}
	for g := 1; g <= 7; g++ {
		jid := "j" + strconv.Itoa(g)
		j := model.Journey{ID: jid, Goal: jid}
		path := "/r" + strconv.Itoa(g)
		scan.Changed = append(scan.Changed, model.Impact{Route: model.Route{Method: "GET", Path: path}, ChangedFuncs: []string{"f" + strconv.Itoa(g)}})
		for s := 1; s <= g; s++ {
			name := "s" + strconv.Itoa(s)
			j.Steps = append(j.Steps, model.Step{Name: name, Method: "GET", Path: path, ExpectStatus: 200})
			run.Findings = append(run.Findings, model.Finding{ID: jid + "." + name + ".p95_ms", Journey: jid, Step: name,
				Metric: model.MetricP95, Unit: "ms", B1: 10, B2: 10, C: 100, Delta: 90, DeltaPct: 900, Class: model.ClassRegression})
		}
		run.Journeys = append(run.Journeys, j)
	}
	plan := PlanIssues(run, scan)
	if len(plan.Groups) != 5 || plan.TotalGroups != 7 || plan.LeftOutGroups != 2 || plan.LeftOutSteps != 3 || plan.RegressedSteps != 28 {
		t.Fatalf("plan: groups %d %+v", len(plan.Groups), plan)
	}
	if n := len(plan.Groups[0].Steps); n != 7 {
		t.Errorf("largest group has %d steps", n)
	}
	if n := len(plan.Groups[4].Steps); n != 3 {
		t.Errorf("fifth group has %d steps", n)
	}
	is, err := Build(run, scan, nil, 1, "a")
	if err != nil || len(is) != 5 {
		t.Fatalf("%v, %d issues", err, len(is))
	}
}

func TestTitleTruncation(t *testing.T) {
	run, scan := groupFixture()
	var funcs []string
	for i := 0; i < 40; i++ {
		funcs = append(funcs, "pkg/very/long/package/name.(*Receiver).MethodNumber"+strconv.Itoa(1000+i))
	}
	scan.Changed = []model.Impact{
		{Route: model.Route{Method: "GET", Path: "/b"}, ChangedFuncs: funcs},
	}
	is, err := Build(run, scan, nil, 1, "a")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, i := range is {
		if n := len([]rune(i.Title)); n >= 200 {
			t.Errorf("title is %d characters: %q", n, i.Title)
		}
		if strings.Contains(i.Title, " via ") {
			found = true
			if !strings.Contains(i.Title, " more: worst j1.s3 p50 latency up 120.0 ms (+120%)") {
				t.Errorf("truncated title lost its tail or the 'and K more' note: %q", i.Title)
			}
			if !strings.Contains(i.Title, " and ") {
				t.Errorf("no 'and K more': %q", i.Title)
			}
		}
	}
	if !found {
		t.Fatalf("no keyed group title in %+v", is)
	}
	// the body still lists every function
	for _, i := range is {
		if strings.Contains(i.Title, " via ") && !strings.Contains(i.Body, funcs[39]) {
			t.Errorf("body does not list all changed functions")
		}
	}
}

func TestIssueJSONTags(t *testing.T) {
	b, err := json.Marshal(Issue{FindingID: "a", Title: "t", Body: "b", Steps: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"finding_id":"a","title":"t","body":"b","steps":["a"]}` {
		t.Errorf("json = %s", b)
	}
}

func TestHeadlines(t *testing.T) {
	ids := func(fs []model.Finding) []string {
		var out []string
		for _, f := range fs {
			out = append(out, f.ID)
		}
		return out
	}
	// steps regressed: one headline per step, app regressions not headlines
	got := ids(Headlines(fixture(), scanFixture()))
	want := []string{"shelf.add_book.p95_ms", "shelf.signup.failures"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("headlines = %v, want %v", got, want)
	}

	// preference order failures, p95, mean, p50
	reg := func(metric string) model.Finding {
		return model.Finding{ID: "j.s." + metric, Journey: "j", Step: "s", Metric: metric, Class: model.ClassRegression}
	}
	for _, tc := range []struct {
		metrics []string
		want    string
	}{
		{[]string{model.MetricP50, model.MetricMean, model.MetricP95, model.MetricFailures}, "j.s.failures"},
		{[]string{model.MetricP50, model.MetricMean, model.MetricP95}, "j.s.p95_ms"},
		{[]string{model.MetricP50, model.MetricMean}, "j.s.mean_ms"},
		{[]string{model.MetricP50}, "j.s.p50_ms"},
	} {
		var run model.Run
		for _, m := range tc.metrics {
			run.Findings = append(run.Findings, reg(m))
		}
		if got := ids(Headlines(run, nil)); len(got) != 1 || got[0] != tc.want {
			t.Errorf("%v: headlines = %v, want %s", tc.metrics, got, tc.want)
		}
	}

	// two steps give two headlines
	run := model.Run{Findings: []model.Finding{
		reg(model.MetricP95),
		{ID: "j.t.p50_ms", Journey: "j", Step: "t", Metric: model.MetricP50, Class: model.ClassRegression},
		{ID: "j.u.p95_ms", Journey: "j", Step: "u", Metric: model.MetricP95, Class: model.ClassNoise},
	}}
	// without a scan both steps share one group
	if got := ids(Headlines(run, nil)); !reflect.DeepEqual(got, []string{"j.s.p95_ms"}) {
		t.Errorf("headlines = %v", got)
	}

	if got := Headlines(model.Run{}, nil); len(got) != 0 {
		t.Errorf("no regressions should give no headlines")
	}
}

func TestAppOnlyIssue(t *testing.T) {
	run := fixture()
	var keep []model.Finding
	for _, f := range run.Findings {
		if f.Journey == "" || f.Class != model.ClassRegression {
			keep = append(keep, f)
		}
	}
	run.Findings = append(keep, model.Finding{ID: "app.peak_rss_mb", Metric: model.MetricRSS, Unit: "MB",
		B1: 48.2, C: 71.9, B2: 48.9, Band: 0.7, Delta: 23, DeltaPct: 47.034, Class: model.ClassRegression, Reason: "rss rule"})
	h := Headlines(run, nil)
	if len(h) != 1 || h[0].ID != "app.peak_rss_mb" {
		t.Fatalf("headlines = %+v, want the peak RSS finding", h)
	}
	is, err := Build(run, nil, nil, 5, "carol")
	if err != nil {
		t.Fatal(err)
	}
	if len(is) != 1 {
		t.Fatalf("got %d issues, want one combined app issue", len(is))
	}
	if is[0].Title != "[Downstream] app: peak memory up 23.0 MB (+47%)" {
		t.Errorf("title = %q", is[0].Title)
	}
	for _, w := range []string{
		"- peak memory: B1 48.2 MB, B2 48.9 MB, C 71.9 MB, noise band 0.7 MB",
		"- CPU time: B1 2.31 s",
		"whole app process",
	} {
		if !strings.Contains(is[0].Body, w) {
			t.Errorf("missing %q in:\n%s", w, is[0].Body)
		}
	}
	if strings.Contains(is[0].Body, "- App process:") {
		t.Errorf("app-only issue should not nest an App process list")
	}
	m, err := ParseMarker(is[0].Body)
	if err != nil || m.FindingID != "app.peak_rss_mb" {
		t.Errorf("marker: %+v, %v", m, err)
	}
}

func TestParseMarkerBareJSON(t *testing.T) {
	is, err := Build(fixture(), scanFixture(), notesFixture(), 12, "alice")
	if err != nil {
		t.Fatal(err)
	}
	line := markerLine(t, is[0].Body)
	js := strings.TrimSuffix(strings.TrimPrefix(line, markerPrefix), markerSuffix)
	fromComment, err := ParseMarker(is[0].Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{js, "  \n" + js + "\n\n"} {
		got, err := ParseMarker(in)
		if err != nil {
			t.Fatalf("bare JSON rejected: %v", err)
		}
		if !reflect.DeepEqual(got, fromComment) {
			t.Errorf("bare JSON parsed differently")
		}
	}
	for name, bad := range map[string]string{
		"bad sha":    `{"v":1,"mr":1,"base":"x","head":"` + headSHA + `","finding":"a"}`,
		"no version": `{"mr":1,"base":"` + baseSHA + `","head":"` + headSHA + `","finding":"a"}`,
		"trailing":   js + ` {}`,
		"truncated":  js[:len(js)-5],
	} {
		if _, err := ParseMarker(bad); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestHeadlineIsLargestAbsoluteDelta(t *testing.T) {
	run := model.Run{Version: 1, Base: baseSHA, Head: headSHA}
	// "fast" has the bigger ratio on a tiny baseline; "slow" lost more milliseconds.
	run.Findings = []model.Finding{
		{ID: "j.fast.p95_ms", Journey: "j", Step: "fast", Metric: model.MetricP95, Unit: "ms",
			B1: 5, B2: 5, C: 30, Delta: 25, DeltaPct: 500, Class: model.ClassRegression, Reason: "rule"},
		{ID: "j.slow.p95_ms", Journey: "j", Step: "slow", Metric: model.MetricP95, Unit: "ms",
			B1: 1000, B2: 1000, C: 1300, Delta: 300, DeltaPct: 30, Class: model.ClassRegression, Reason: "rule"},
	}
	plan := PlanIssues(run, nil)
	if len(plan.Groups) != 1 || plan.Groups[0].Headline.ID != "j.slow.p95_ms" {
		t.Fatalf("headline = %+v", plan.Groups)
	}
	if got := plan.Groups[0].title(run); !strings.Contains(got, "worst j.slow p95 latency up 300.0 ms") {
		t.Errorf("title = %q", got)
	}
	// A failures finding still wins over any latency change.
	run.Findings = append(run.Findings, model.Finding{ID: "j.other.failures", Journey: "j", Step: "other",
		Metric: model.MetricFailures, Unit: "count", C: 2, Delta: 2, Class: model.ClassRegression, Reason: "rule"})
	if got := PlanIssues(run, nil).Groups[0].Headline.ID; got != "j.other.failures" {
		t.Errorf("failures headline = %q", got)
	}
}
