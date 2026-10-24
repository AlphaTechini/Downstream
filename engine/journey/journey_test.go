package journey

import (
	"encoding/json"
	"strings"
	"testing"

	"downstream/engine/model"
)

var testRoutes = []model.Route{
	{Method: "GET", Path: "/health"},
	{Method: "POST", Path: "/users"},
	{Method: "POST", Path: "/login"},
	{Method: "GET", Path: "/books/{id}"},
	{Method: "GET", Path: "/files/{path...}"},
	{Method: "", Path: "/any"},
	{Method: "GET", Path: "/{$}"},
	{Method: "GET", Path: "/static/"},
	{Method: "GET", Path: "/shelves/{id}/books/{$}"},
}

func validFile() model.JourneyFile {
	return model.JourneyFile{
		Version: 1,
		Journeys: []model.Journey{{
			ID: "signup_login", Goal: "A new user signs up and logs in.", WhyIncluded: "Touches the password hash.",
			Steps: []model.Step{
				{Name: "signup", Method: "POST", Path: "/users", ExpectStatus: 201,
					Body:    json.RawMessage(`{"name":"u_{{uniq}}","tags":["a","{{uniq}}"]}`),
					Extract: map[string]string{"user_id": "/id"}},
				{Name: "login", Method: "POST", Path: "/login", ExpectStatus: 200,
					Headers: map[string]string{"X-User": "{{user_id}}", "Accept": "application/json"},
					Body:    json.RawMessage(`{"name":"u_{{uniq}}"}`),
					Extract: map[string]string{"token": "/token"}},
				{Name: "book", Method: "GET", Path: "/books/{{user_id}}?x=1", ExpectStatus: 200,
					Headers: map[string]string{"Authorization": "Bearer {{token}}"}},
			},
		}},
	}
}

func TestValidateAccepts(t *testing.T) {
	if err := Validate(validFile(), testRoutes); err != nil {
		t.Fatalf("valid file rejected: %v", err)
	}
}

func TestValidateRules(t *testing.T) {
	long := strings.Repeat("x", 201)
	cases := []struct {
		name   string
		mutate func(f *model.JourneyFile)
		want   string // substring of the error, which must also name the journey/step
	}{
		{"version", func(f *model.JourneyFile) { f.Version = 2 }, "version must be 1"},
		{"no journeys", func(f *model.JourneyFile) { f.Journeys = nil }, "1 to 20 journeys"},
		{"too many journeys", func(f *model.JourneyFile) {
			for i := 0; i < 20; i++ {
				j := f.Journeys[0]
				j.ID = "j" + string(rune('a'+i))
				f.Journeys = append(f.Journeys, j)
			}
		}, "1 to 20 journeys"},
		{"journey id", func(f *model.JourneyFile) { f.Journeys[0].ID = "Bad-ID" }, "id must match"},
		{"journey id empty", func(f *model.JourneyFile) { f.Journeys[0].ID = "" }, "id must match"},
		{"journey id long", func(f *model.JourneyFile) { f.Journeys[0].ID = strings.Repeat("a", 41) }, "id must match"},
		{"duplicate journey", func(f *model.JourneyFile) { f.Journeys = append(f.Journeys, f.Journeys[0]) }, "more than one journey"},
		{"goal empty", func(f *model.JourneyFile) { f.Journeys[0].Goal = "" }, "goal must be"},
		{"goal long", func(f *model.JourneyFile) { f.Journeys[0].Goal = long }, "goal must be"},
		{"why empty", func(f *model.JourneyFile) { f.Journeys[0].WhyIncluded = "  " }, "why_included must be"},
		{"why long", func(f *model.JourneyFile) { f.Journeys[0].WhyIncluded = long }, "why_included must be"},
		{"no steps", func(f *model.JourneyFile) { f.Journeys[0].Steps = nil }, "1 to 12 steps"},
		{"too many steps", func(f *model.JourneyFile) {
			s := f.Journeys[0].Steps[2]
			f.Journeys[0].Steps = nil
			for i := 0; i < 13; i++ {
				s.Name = "s" + string(rune('a'+i))
				s.Headers = nil
				s.Path = "/health"
				f.Journeys[0].Steps = append(f.Journeys[0].Steps, s)
			}
		}, "1 to 12 steps"},
		{"step name", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Name = "Sign Up" }, "name must match"},
		{"duplicate step", func(f *model.JourneyFile) { f.Journeys[0].Steps[1].Name = "signup" }, "more than one step"},
		{"method", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Method = "TRACE" }, "method must be one of"},
		{"method lower", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Method = "post" }, "method must be one of"},
		{"path slash", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Path = "users" }, "must start with /"},
		{"path scheme", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Path = "/redirect?u=http://evil" }, "://"},
		{"path dots", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Path = "/books/../users" }, ".."},
		{"path double slash", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Path = "//evil.example/users" }, "not allowed"},
		{"path space", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Path = "/us ers" }, "not allowed"},
		{"path unknown route", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Path = "/nope" }, "matches no route"},
		{"wrong method for route", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Method = "DELETE" }, "matches no route"},
		{"header not allowed", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Headers = map[string]string{"Cookie": "a=b"} }, "not allowed"},
		{"header host", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Headers = map[string]string{"Host": "example.com"} }, "Host header"},
		{"header newline", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Headers = map[string]string{"X-A": "a\r\nB: c"} }, "invalid value"},
		{"header duplicate case", func(f *model.JourneyFile) {
			f.Journeys[0].Steps[0].Headers = map[string]string{"X-A": "1", "x-a": "2"}
		}, "same header"},
		{"header placeholder unknown", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Headers = map[string]string{"X-A": "{{nope}}"} }, "{{nope}}"},
		{"body scalar", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Body = json.RawMessage(`"text"`) }, "object or array"},
		{"body null", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Body = json.RawMessage(`null`) }, "object or array"},
		{"body too big", func(f *model.JourneyFile) {
			f.Journeys[0].Steps[0].Body = json.RawMessage(`{"a":"` + strings.Repeat("x", 4100) + `"}`)
		}, "limit is 4096"},
		{"body placeholder unknown", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Body = json.RawMessage(`{"a":[{"b":"{{later}}"}]}`) }, "{{later}}"},
		{"body key placeholder", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Body = json.RawMessage(`{"{{uniq}}":1}`) }, "body keys"},
		{"body unbalanced", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Body = json.RawMessage(`{"a":"{{uniq"}`) }, "unbalanced"},
		{"status low", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].ExpectStatus = 99 }, "expect_status"},
		{"status high", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].ExpectStatus = 600 }, "expect_status"},
		{"path placeholder from later step", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Path = "/books/{{token}}" }, "{{token}}"},
		{"placeholder uses own extract", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Body = json.RawMessage(`{"a":"{{user_id}}"}`) }, "{{user_id}}"},
		{"placeholder bad name", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Body = json.RawMessage(`{"a":"{{Bad Name}}"}`) }, "invalid placeholder"},
		{"extract name", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Extract = map[string]string{"1bad": "/id"} }, "extract name"},
		{"extract uniq", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Extract = map[string]string{"uniq": "/id"} }, "already defined"},
		{"extract redefined", func(f *model.JourneyFile) { f.Journeys[0].Steps[1].Extract = map[string]string{"user_id": "/id"} }, "already defined"},
		{"pointer no slash", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Extract = map[string]string{"user_id": "id"} }, "must start with /"},
		{"pointer empty", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Extract = map[string]string{"user_id": ""} }, "must start with /"},
		{"pointer bad escape", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Extract = map[string]string{"user_id": "/a~2b"} }, "~"},
		{"pointer trailing tilde", func(f *model.JourneyFile) { f.Journeys[0].Steps[0].Extract = map[string]string{"user_id": "/a~"} }, "~"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := validFile()
			c.mutate(&f)
			err := Validate(f, testRoutes)
			if err == nil {
				t.Fatalf("expected an error containing %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not contain %q", err, c.want)
			}
		})
	}
}

func TestValidateErrorsNameJourneyAndStep(t *testing.T) {
	f := validFile()
	f.Journeys[0].Steps[1].ExpectStatus = 0
	err := Validate(f, testRoutes)
	if err == nil || !strings.Contains(err.Error(), `journey "signup_login" step "login"`) {
		t.Fatalf("error does not name journey and step: %v", err)
	}
}

func TestValidateJoinsAllProblems(t *testing.T) {
	f := validFile()
	f.Version = 3
	f.Journeys[0].Goal = ""
	f.Journeys[0].Steps[0].ExpectStatus = 700
	f.Journeys[0].Steps[2].Path = "/nope"
	err := Validate(f, testRoutes)
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"version must be 1", "goal must be", "expect_status", "matches no route"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

func TestValidateEmptyRouteTable(t *testing.T) {
	if err := Validate(validFile(), nil); err == nil || !strings.Contains(err.Error(), "route table is empty") {
		t.Fatalf("got %v", err)
	}
}

func TestMatchRoute(t *testing.T) {
	cases := []struct {
		method, path string
		want         string // matched route path, "" for no match
	}{
		{"GET", "/health", "/health"},
		{"POST", "/health", ""},
		{"HEAD", "/health", "/health"},
		{"HEAD", "/users", ""},
		{"POST", "/users", "/users"},
		{"GET", "/health?x=1&y=/z", "/health"},
		{"GET", "/books/42", "/books/{id}"},
		{"GET", "/books/42?expand=1", "/books/{id}"},
		{"GET", "/books/{{book_id}}", "/books/{id}"},
		{"GET", "/books/", ""},
		{"GET", "/books", ""},
		{"GET", "/books/1/2", ""},
		{"GET", "/files/a/b/c", "/files/{path...}"},
		{"GET", "/files/a", "/files/{path...}"},
		{"GET", "/files/", "/files/{path...}"},
		{"GET", "/files/{{p}}/x", "/files/{path...}"},
		{"GET", "/files", ""},
		{"DELETE", "/any", "/any"},
		{"PATCH", "/any", "/any"},
		{"GET", "/", "/{$}"},
		{"GET", "/?a=b", "/{$}"},
		{"GET", "/static/css/site.css", "/static/"},
		{"GET", "/static/", "/static/"},
		{"GET", "/static", ""},
		{"GET", "/shelves/9/books/", "/shelves/{id}/books/{$}"},
		{"GET", "/shelves/9/books", ""},
		{"GET", "/shelves/9/books/x", ""},
		{"GET", "/health/{{x}}", ""},
		{"POST", "/{{x}}", ""},
		{"GET", "no-slash", ""},
	}
	for _, c := range cases {
		r, ok := MatchRoute(c.method, c.path, testRoutes)
		if c.want == "" {
			if ok {
				t.Errorf("%s %s: unexpected match %s", c.method, c.path, r.Path)
			}
			continue
		}
		if !ok || r.Path != c.want {
			t.Errorf("%s %s: got %q ok=%v, want %q", c.method, c.path, r.Path, ok, c.want)
		}
	}
}

func TestMatchRoutePlaceholderIsNotLiteral(t *testing.T) {
	routes := []model.Route{{Method: "GET", Path: "/books/new"}}
	if _, ok := MatchRoute("GET", "/books/{{x}}", routes); ok {
		t.Fatal("a placeholder segment must not match a literal segment")
	}
}

func TestParse(t *testing.T) {
	good := `{"version":1,"journeys":[{"id":"a","goal":"g","why_included":"w","steps":[{"name":"s","method":"GET","path":"/health","expect_status":200}]}]}`
	f, err := Parse([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if f.Journeys[0].Steps[0].Path != "/health" {
		t.Fatalf("parsed wrong: %+v", f)
	}
	bad := map[string]string{
		"unknown top field":  `{"version":1,"journeys":[],"extra":1}`,
		"unknown step field": `{"version":1,"journeys":[{"id":"a","goal":"g","why_included":"w","steps":[{"name":"s","method":"GET","path":"/","expect_status":200,"retry":3}]}]}`,
		"trailing data":      good + `{}`,
		"not json":           `nope`,
		"wrong type":         `{"version":"1","journeys":[]}`,
		"empty":              ``,
	}
	for name, in := range bad {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestSchemaIsValidJSON(t *testing.T) {
	var v map[string]any
	if err := json.Unmarshal([]byte(Schema()), &v); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	if v["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("wrong $schema: %v", v["$schema"])
	}
	if !strings.Contains(Schema(), "\n  ") {
		t.Error("schema is not indented")
	}
	for _, want := range []string{"expect_status", "extract", "why_included", "TRACE"} {
		if strings.Contains(Schema(), want) != (want != "TRACE") {
			t.Errorf("schema content check failed for %q", want)
		}
	}
}
