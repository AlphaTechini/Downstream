package scan

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const routesSrc = `package main

import (
	"net/http"

	"example.com/app/internal/web"
)

type Server struct{}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("/books", listBooks)
	mux.Handle("POST /books", s.requireAuth(http.HandlerFunc(s.handleCreate)))
	mux.Handle("DELETE /books/{id}", logging(s.requireAuth(http.HandlerFunc(s.handleDelete))))
	mux.HandleFunc("GET /inline", func(w http.ResponseWriter, r *http.Request) {
		helper()
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("."))))
	mux.HandleFunc("GET /ext", web.Ext)
	mux.HandleFunc(dynamicPattern, listBooks)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {}
func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {}
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {}
func (s *Server) requireAuth(next http.Handler) http.Handler          { return next }

func logging(next http.Handler) http.Handler { return next }
func listBooks(w http.ResponseWriter, r *http.Request) {}
func helper()                                          {}

var dynamicPattern = "GET /dyn"
`

func TestRoutes(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"main.go":                  routesSrc,
		"main_test.go":             "package main\nfunc init() { http.HandleFunc(\"/test\", nil) }\n",
		"testdata/x.go":            "package x\nfunc init() { m.HandleFunc(\"/td\", nil) }\n",
		"internal/web/web.go":      "package web\nimport \"net/http\"\nfunc Ext(w http.ResponseWriter, r *http.Request) {}\n",
		"internal/web/web_test.go": "package web\n",
	})
	got, err := Routes(dir)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		Method, Path, Handler string
		Middleware            []string
	}
	var rows []row
	for _, r := range got {
		rows = append(rows, row{r.Method, r.Path, r.Handler, r.Middleware})
	}
	want := []row{
		{"GET", "/health", "(Server).handleHealth", nil},
		{"", "/books", "listBooks", nil},
		{"POST", "/books", "(Server).handleCreate", []string{"(Server).requireAuth"}},
		{"DELETE", "/books/{id}", "(Server).handleDelete", []string{"logging", "(Server).requireAuth"}},
		{"GET", "/inline", "func@" + filepath.ToSlash(dir) + "/main.go:17", nil},
		{"GET", "/static/", "http.FileServer", []string{"http.StripPrefix"}},
		{"GET", "/ext", "internal/web.Ext", nil},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("routes differ\n got: %+v\nwant: %+v", rows, want)
	}
	if got[0].File != filepath.ToSlash(dir)+"/main.go" || got[0].Line != 13 {
		t.Errorf("file and line: %s:%d", got[0].File, got[0].Line)
	}
}

func TestSplitPattern(t *testing.T) {
	for in, want := range map[string][2]string{
		"GET /books/{id}":     {"GET", "/books/{id}"},
		"/books":              {"", "/books"},
		"example.com/x":       {"", "/x"},
		"POST   /a/{rest...}": {"POST", "/a/{rest...}"},
	} {
		m, p := splitPattern(in)
		if m != want[0] || p != want[1] {
			t.Errorf("%q: got %q %q, want %q %q", in, m, p, want[0], want[1])
		}
	}
}

func TestRoutesSyntaxError(t *testing.T) {
	dir := writeTree(t, map[string]string{"a.go": "package a\nfunc {"})
	if _, err := Routes(dir); err == nil {
		t.Fatal("want a parse error")
	}
}

func TestRoutesEmpty(t *testing.T) {
	dir := writeTree(t, map[string]string{"a.go": "package a\n"})
	got, err := Routes(dir)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %v %v, want an empty non-nil slice", got, err)
	}
}
