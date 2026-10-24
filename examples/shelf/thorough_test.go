package shelf

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"downstream/engine/journey"
	"downstream/engine/model"
	"downstream/engine/scan"
)

func loadThorough(t *testing.T) (model.JourneyFile, []model.Route) {
	t.Helper()
	raw, err := os.ReadFile("journeys.thorough.json")
	if err != nil {
		t.Fatal(err)
	}
	jf, err := journey.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := scan.Routes(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) == 0 {
		t.Fatal("no routes found in examples/shelf")
	}
	return jf, routes
}

func TestThoroughJourneysValidate(t *testing.T) {
	jf, routes := loadThorough(t)
	if err := journey.Validate(jf, routes); err != nil {
		t.Fatal(err)
	}
	statuses := map[int]bool{}
	for _, j := range jf.Journeys {
		for _, s := range j.Steps {
			statuses[s.ExpectStatus] = true
		}
	}
	for _, want := range []int{200, 201, 400, 401, 404, 409} {
		if !statuses[want] {
			t.Errorf("no step expects %d", want)
		}
	}
}

// Every route the app can answer with a non-2xx status is covered by a step that
// expects one. GET /health and GET /books cannot fail by design (no input, no
// token), so Lint reports them: the first as uncovered, the second as 2xx-only.
func TestThoroughJourneysLint(t *testing.T) {
	jf, routes := loadThorough(t)
	var scanned model.Scan
	scanned.Routes = routes
	var cannotFail []model.Impact
	for _, r := range routes {
		im := model.Impact{Route: r}
		if (r.Method == "GET" && r.Path == "/health") || (r.Method == "GET" && r.Path == "/books") {
			cannotFail = append(cannotFail, im)
			continue
		}
		scanned.Changed = append(scanned.Changed, im)
	}
	if got := journey.Lint(jf, scanned); len(got) != 0 {
		t.Errorf("Lint warnings: %q", got)
	}

	scanned.Changed = append(scanned.Changed, cannotFail...)
	want := []string{
		"changed route GET /books is covered only by steps that expect a 2xx status; add a negative or edge-input step",
		"changed route GET /health is not covered by any journey",
	}
	got := journey.Lint(jf, scanned)
	if len(got) != len(want) {
		t.Fatalf("Lint = %q", got)
	}
	seen := map[string]bool{}
	for _, g := range got {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Errorf("missing warning %q in %q", w, got)
		}
	}
}

// TestThoroughJourneysReplay runs the set against a fresh server and requires every
// step to get its expected status. The set runs many times on one store, as the
// iterations of a pass do, and the book list must end up cut off at its page size.
func TestThoroughJourneysReplay(t *testing.T) {
	jf, _ := loadThorough(t)
	c := newClient(t)
	const rounds = 10
	for round := 1; round <= rounds; round++ {
		for _, j := range jf.Journeys {
			replayJourney(t, c, j, "t"+strconv.Itoa(round))
		}
	}
	st, out := c.do("GET", "/books", "", nil)
	c.want(200, st, out)
	if n := len(out["books"].([]any)); n != maxListed {
		t.Errorf("book list holds %d after %d rounds, want the page size %d", n, rounds, maxListed)
	}
}

func replayJourney(t *testing.T, c *client, j model.Journey, uniq string) {
	t.Helper()
	vars := map[string]string{"uniq": uniq}
	fill := func(s string) string {
		for k, v := range vars {
			s = strings.ReplaceAll(s, "{{"+k+"}}", v)
		}
		return s
	}
	for _, st := range j.Steps {
		var body io.Reader
		if len(st.Body) > 0 {
			body = bytes.NewReader([]byte(fill(string(st.Body))))
		}
		req, err := http.NewRequest(st.Method, c.srv.URL+fill(st.Path), body)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range st.Headers {
			req.Header.Set(k, fill(v))
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		_ = json.NewDecoder(resp.Body).Decode(&doc)
		resp.Body.Close()
		if resp.StatusCode != st.ExpectStatus {
			t.Fatalf("%s/%s: status %d, want %d (%v)", j.ID, st.Name, resp.StatusCode, st.ExpectStatus, doc)
		}
		for name, ptr := range st.Extract {
			v, ok := pointerAt(doc, ptr)
			if !ok {
				t.Fatalf("%s/%s: nothing at %s", j.ID, st.Name, ptr)
			}
			vars[name] = v
		}
	}
}

// pointerAt is an RFC 6901 lookup through objects and arrays that returns a scalar as text.
func pointerAt(doc any, ptr string) (string, bool) {
	cur := doc
	for _, part := range strings.Split(strings.TrimPrefix(ptr, "/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[part]
			if !ok {
				return "", false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return "", false
			}
			cur = node[i]
		default:
			return "", false
		}
	}
	switch v := cur.(type) {
	case string:
		return v, true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	}
	return "", false
}
