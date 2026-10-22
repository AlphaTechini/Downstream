package k6gen

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"downstream/engine/model"
)

func sampleFile() model.JourneyFile {
	return model.JourneyFile{
		Version: 1,
		Journeys: []model.Journey{
			{ID: "signup", Goal: "g", WhyIncluded: "w", Steps: []model.Step{
				{Name: "create", Method: "POST", Path: "/users", ExpectStatus: 201,
					Body:    json.RawMessage(`{"name":"u_{{uniq}}","nested":{"tags":["{{uniq}}"]}}`),
					Extract: map[string]string{"user_id": "/id", "first": "/list/0/k~1x"}},
				{Name: "fetch", Method: "GET", Path: "/users/{{user_id}}?v={{first}}", ExpectStatus: 200,
					Headers: map[string]string{"X-Token": "t-{{uniq}}"}},
			}},
			{ID: "1numeric", Goal: "g", WhyIncluded: "w", Steps: []model.Step{
				{Name: "ok", Method: "GET", Path: "/health", ExpectStatus: 200},
				{Name: "bad", Method: "GET", Path: "/broken", ExpectStatus: 200},
				{Name: "never", Method: "GET", Path: "/health", ExpectStatus: 200},
			}},
			{ID: "missing", Goal: "g", WhyIncluded: "w", Steps: []model.Step{
				{Name: "x", Method: "GET", Path: "/health", ExpectStatus: 200, Extract: map[string]string{"v": "/nope"}},
				{Name: "y", Method: "GET", Path: "/health", ExpectStatus: 200},
			}},
		},
	}
}

func opts() Options {
	return Options{BaseURL: "http://127.0.0.1:8080", VUs: 2, Iterations: 3, MaxDuration: 30 * time.Second, RunID: "abc123"}
}

func TestScriptContents(t *testing.T) {
	s, err := Script(sampleFile(), opts())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"import http from 'k6/http';",
		`const BASE = "http://127.0.0.1:8080";`,
		`const RUN_ID = "abc123";`,
		"const VUS = 2;", "const ITERATIONS = 3;", "const MAX_DURATION = '30s';",
		"export function j_signup() { runJourney(JOURNEYS[0]); }",
		"export function j_1numeric() { runJourney(JOURNEYS[1]); }",
		"export function j_missing() { runJourney(JOURNEYS[2]); }",
		"export function handleSummary(data)",
		"per-vu-iterations",
		`"id":"signup"`,
		"summaryTrendStats: ['count', 'avg', 'med', 'p(95)', 'min', 'max']",
		"'ds_' + id + '_duration'",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q", want)
		}
	}
}

func TestScriptKeepsModelTextInsideLiteral(t *testing.T) {
	f := sampleFile()
	f.Journeys[0].Goal = "evil `); process.exit(); //  and   </script>"
	s, err := Script(f, opts())
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(s, "  ") {
		t.Error("raw line separators in the script")
	}
	if strings.Contains(s, "process.exit(); //\n") || strings.Count(s, "process.exit()") != 1 {
		t.Error("model text escaped its string literal")
	}
	if strings.Contains(s, "</script>") {
		t.Error("angle brackets are not escaped")
	}
}

func TestScriptRejectsBadBaseURL(t *testing.T) {
	bad := []string{
		"", "http://localhost:8080", "http://example.com:80", "https://127.0.0.1:8080",
		"http://127.0.0.2:8080", "http://127.0.0.1", "http://127.0.0.1:0x50",
		"http://127.0.0.1:8080/path", "http://127.0.0.1:8080?x=1", "http://user@127.0.0.1:8080",
		"http://127.0.0.1.evil.example:8080", "http://[::1]:8080", "//127.0.0.1:8080",
		"http://127.0.0.1:8080@evil.example", "http://evil.example:80@127.0.0.1:8080/x", "http://127.0.0.1:99999",
		"http://127.0.0.1:080",
	}
	for _, u := range bad {
		o := opts()
		o.BaseURL = u
		if _, err := Script(sampleFile(), o); err == nil {
			t.Errorf("BaseURL %q was accepted", u)
		}
	}
	for _, u := range []string{"http://127.0.0.1:8080", "http://127.0.0.1:8080/", "http://127.0.0.1:1"} {
		o := opts()
		o.BaseURL = u
		if _, err := Script(sampleFile(), o); err != nil {
			t.Errorf("BaseURL %q rejected: %v", u, err)
		}
	}
}

func TestScriptRejectsBadOptionsAndNames(t *testing.T) {
	cases := map[string]func(*model.JourneyFile, *Options){
		"vus":         func(_ *model.JourneyFile, o *Options) { o.VUs = 0 },
		"iterations":  func(_ *model.JourneyFile, o *Options) { o.Iterations = 0 },
		"duration":    func(_ *model.JourneyFile, o *Options) { o.MaxDuration = 0 },
		"run id":      func(_ *model.JourneyFile, o *Options) { o.RunID = "A-B" },
		"journey id":  func(f *model.JourneyFile, _ *Options) { f.Journeys[0].ID = "a'b" },
		"step name":   func(f *model.JourneyFile, _ *Options) { f.Journeys[0].Steps[0].Name = "a b" },
		"no journeys": func(f *model.JourneyFile, _ *Options) { f.Journeys = nil },
		"no steps":    func(f *model.JourneyFile, _ *Options) { f.Journeys[0].Steps = nil },
		"duplicate":   func(f *model.JourneyFile, _ *Options) { f.Journeys = append(f.Journeys, f.Journeys[0]) },
		"collision": func(f *model.JourneyFile, _ *Options) {
			f.Journeys = []model.Journey{
				{ID: "a_", Steps: []model.Step{{Name: "__b"}}},
				{ID: "a", Steps: []model.Step{{Name: "___b"}}},
			}
		},
	}
	for name, mut := range cases {
		f := sampleFile()
		o := opts()
		mut(&f, &o)
		if _, err := Script(f, o); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestParseSummary(t *testing.T) {
	good := `[{"journey":"a","step":"b","count":4,"failures":1,"p50_ms":1.5,"p95_ms":2.5,"mean_ms":1.75}]`
	got, err := ParseSummary([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	want := model.StepMetrics{Journey: "a", Step: "b", Count: 4, Failures: 1, P50: 1.5, P95: 2.5, Mean: 1.75}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v", got)
	}
	for _, bad := range []string{``, `[]`, `{}`, `[{"journey":"a","step":"b","extra":1}]`, `[{"journey":"","step":"b"}]`, `[{"journey":"a","step":"b","count":-1}]`, `[{"journey":"a"`} {
		if _, err := ParseSummary([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func findK6() string {
	if p, err := exec.LookPath("k6"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, "go", "bin", "k6")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func TestK6Integration(t *testing.T) {
	k6 := findK6()
	if k6 == "" {
		t.Skip("k6 not installed")
	}
	if testing.Short() {
		t.Skip("short mode")
	}

	var mu sync.Mutex
	var created int
	fetched := map[string]string{} // path+query seen by GET /users/{id}
	var tokens []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"ok":true}`) })
	mux.HandleFunc("GET /broken", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 500) })
	mux.HandleFunc("POST /users", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name   string `json:"name"`
			Nested struct {
				Tags []string `json:"tags"`
			} `json:"nested"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || r.Header.Get("Content-Type") != "application/json" ||
			!strings.HasPrefix(body.Name, "u_abc123_") || len(body.Nested.Tags) != 1 || body.Name != "u_"+body.Nested.Tags[0] {
			http.Error(w, "bad body", 400)
			return
		}
		mu.Lock()
		created++
		id := fmt.Sprintf("id%d", created)
		mu.Unlock()
		w.WriteHeader(201)
		fmt.Fprintf(w, `{"id":%q,"list":[{"k/x":"v1"}]}`, id)
	})
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fetched[r.PathValue("id")] = r.URL.RawQuery
		tokens = append(tokens, r.Header.Get("X-Token"))
		mu.Unlock()
		fmt.Fprint(w, `{}`)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(mux)
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	defer srv.Close()

	o := opts()
	o.BaseURL = "http://" + ln.Addr().String()
	script, err := Script(sampleFile(), o)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "script.js"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, k6, "run", "-q", "--no-color", "--no-usage-report", "--log-output=none", "script.js")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "K6_NO_USAGE_REPORT=true")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("k6: %v\n%s", err, out)
	} else if len(strings.TrimSpace(string(out))) != 0 {
		t.Errorf("k6 printed to stdout/stderr: %q", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "steps.json"))
	if err != nil {
		t.Fatal(err)
	}
	steps, err := ParseSummary(data)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]model.StepMetrics{}
	for _, s := range steps {
		by[s.Journey+"/"+s.Step] = s
	}
	if len(by) != 7 {
		t.Fatalf("want 7 steps, got %d: %s", len(by), data)
	}
	// 2 VUs x 3 iterations = 6 iterations per journey.
	expect := map[string][2]int{
		"signup/create":  {6, 0},
		"signup/fetch":   {6, 0},
		"1numeric/ok":    {6, 0},
		"1numeric/bad":   {6, 6},
		"1numeric/never": {0, 0},
		"missing/x":      {6, 6},
		"missing/y":      {0, 0},
	}
	for k, w := range expect {
		got, ok := by[k]
		if !ok {
			t.Errorf("%s missing from summary", k)
			continue
		}
		if got.Count != w[0] || got.Failures != w[1] {
			t.Errorf("%s: count %d failures %d, want %d and %d", k, got.Count, got.Failures, w[0], w[1])
		}
		if got.Count > 0 && (got.P95 <= 0 || got.P50 <= 0 || got.Mean <= 0 || got.P95 < got.P50) {
			t.Errorf("%s: odd latencies %+v", k, got)
		}
		if got.Count == 0 && (got.P95 != 0 || got.Mean != 0) {
			t.Errorf("%s: latencies without samples %+v", k, got)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(fetched) != 6 {
		t.Errorf("extraction: fetched %d distinct ids, want 6: %v", len(fetched), fetched)
	}
	for id, q := range fetched {
		if !strings.HasPrefix(id, "id") || q != "v=v1" {
			t.Errorf("extraction: id %q query %q", id, q)
		}
	}
	seen := map[string]bool{}
	for _, tok := range tokens {
		if !strings.HasPrefix(tok, "t-abc123_") || seen[tok] {
			t.Errorf("uniq not substituted or not unique: %q", tok)
		}
		seen[tok] = true
	}
}
