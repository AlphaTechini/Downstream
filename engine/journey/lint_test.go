package journey

import (
	"reflect"
	"testing"

	"downstream/engine/model"
)

func lintRoutes() []model.Route {
	return []model.Route{
		{Method: "POST", Path: "/books"},
		{Method: "GET", Path: "/books"},
		{Method: "GET", Path: "/books/{id}"},
		{Method: "DELETE", Path: "/books/{id}"},
		{Method: "", Path: "/ping"},
	}
}

func lintScan(changed, affected []int) model.Scan {
	rs := lintRoutes()
	s := model.Scan{Routes: rs}
	for _, i := range changed {
		s.Changed = append(s.Changed, model.Impact{Route: rs[i]})
	}
	for _, i := range affected {
		s.Affected = append(s.Affected, model.Impact{Route: rs[i]})
	}
	return s
}

func lintFile(steps ...model.Step) model.JourneyFile {
	return model.JourneyFile{Version: 1, Journeys: []model.Journey{{ID: "j", Goal: "g", WhyIncluded: "w", Steps: steps}}}
}

func st(name, method, path string, status int) model.Step {
	return model.Step{Name: name, Method: method, Path: path, ExpectStatus: status}
}

func TestLint(t *testing.T) {
	tests := []struct {
		name string
		file model.JourneyFile
		scan model.Scan
		want []string
	}{
		{
			name: "changed route with a negative step is clean",
			file: lintFile(st("a", "POST", "/books", 201), st("b", "POST", "/books", 401)),
			scan: lintScan([]int{0}, nil),
		},
		{
			name: "changed route covered only by 2xx",
			file: lintFile(st("a", "POST", "/books", 201), st("b", "POST", "/books", 200)),
			scan: lintScan([]int{0}, nil),
			want: []string{"changed route POST /books is covered only by steps that expect a 2xx status; add a negative or edge-input step"},
		},
		{
			name: "changed route not covered",
			file: lintFile(st("a", "GET", "/books", 200)),
			scan: lintScan([]int{0}, nil),
			want: []string{"changed route POST /books is not covered by any journey"},
		},
		{
			name: "affected route needs only to be reached",
			file: lintFile(st("a", "GET", "/books", 200)),
			scan: lintScan(nil, []int{1}),
		},
		{
			name: "affected route not covered",
			file: lintFile(st("a", "POST", "/books", 201)),
			scan: lintScan(nil, []int{1}),
			want: []string{"affected route GET /books is not covered by any journey"},
		},
		{
			name: "placeholder and query string still match the route",
			file: lintFile(st("a", "GET", "/books/{{id}}?x=1", 200), st("b", "DELETE", "/books/{{id}}", 404)),
			scan: lintScan([]int{2, 3}, nil),
			want: []string{"changed route GET /books/{id} is covered only by steps that expect a 2xx status; add a negative or edge-input step"},
		},
		{
			name: "a 3xx counts as not a success",
			file: lintFile(st("a", "GET", "/books", 301)),
			scan: lintScan([]int{1}, nil),
		},
		{
			name: "route without a method",
			file: lintFile(st("a", "GET", "/ping", 200)),
			scan: lintScan([]int{4}, nil),
			want: []string{"changed route /ping is covered only by steps that expect a 2xx status; add a negative or edge-input step"},
		},
		{
			name: "a route in both lists is reported once, as changed",
			file: lintFile(st("a", "GET", "/health", 200)),
			scan: lintScan([]int{0}, []int{0}),
			want: []string{"changed route POST /books is not covered by any journey"},
		},
		{
			name: "findings come changed first, then affected",
			file: lintFile(),
			scan: lintScan([]int{2}, []int{1}),
			want: []string{
				"changed route GET /books/{id} is not covered by any journey",
				"affected route GET /books is not covered by any journey",
			},
		},
		{
			name: "no scan impact, no findings",
			file: lintFile(st("a", "GET", "/books", 200)),
			scan: lintScan(nil, nil),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Lint(tc.file, tc.scan)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Lint = %q\nwant   %q", got, tc.want)
			}
		})
	}
}

func TestLintNeverPanicsOnUnmatchedSteps(t *testing.T) {
	f := lintFile(st("a", "GET", "no-slash", 200), st("b", "TRACE", "/books", 200))
	if got := Lint(f, lintScan([]int{0}, nil)); len(got) != 1 {
		t.Errorf("Lint = %q, want one finding", got)
	}
}
