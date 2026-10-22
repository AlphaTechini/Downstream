package scan

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"downstream/engine/model"
)

const appSrc = `package main

import (
	"encoding/json"
	"net/http"
)

const saltRounds = 3

type Store struct{ books []string }

func (s *Store) List() []string { return s.books }

var sessions = map[string]bool{}

func hashSecret(secret string) string {
	out := secret
	for i := 0; i < saltRounds; i++ {
		out = out + "x"
	}
	return out
}

func verifyToken(tok string) bool { return hashSecret(tok) != "" }

func markSession(tok string) {
	sessions[tok] = true
}

func writeJSON(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }

type Server struct{ store *Store }

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	markSession(hashSecret(r.FormValue("pw")))
	writeJSON(w, "ok")
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, sessions[r.Header.Get("Authorization")])
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.store.List())
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, len(s.store.List()))
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, "deleted")
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !verifyToken(r.Header.Get("Authorization")) {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("GET /me", s.handleMe)
	mux.HandleFunc("GET /books", s.handleList)
	mux.Handle("POST /books", s.requireAuth(http.HandlerFunc(s.handleCreate)))
	mux.Handle("DELETE /books/{id}", s.requireAuth(http.HandlerFunc(s.handleDelete)))
	return mux
}

func main() {
	s := &Server{store: &Store{}}
	_ = http.ListenAndServe("127.0.0.1:0", s.routes())
}
`

var gitEnv = []string{
	"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid",
	"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid",
	"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), gitEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// commitFiles writes files (a nil value deletes the file), commits and returns the new commit.
func commitFiles(t *testing.T, dir string, files map[string]*string) string {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if content == nil {
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(*content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "-m", "change")
	return gitT(t, dir, "rev-parse", "HEAD")
}

func str(s string) *string { return &s }

func newApp(t *testing.T) (dir, base string) {
	t.Helper()
	dir = t.TempDir()
	gitT(t, dir, "init", "-q")
	base = commitFiles(t, dir, map[string]*string{
		"go.mod":       str("module example.com/shelf\n\ngo 1.22\n"),
		"app/main.go":  str(appSrc),
		"docs/note.md": str("hello\n"),
	})
	return dir, base
}

func find(list []model.Impact, method, path string) *model.Impact {
	for i := range list {
		if list[i].Method == method && list[i].Path == path {
			return &list[i]
		}
	}
	return nil
}

func anyContains(reasons []string, sub string) bool {
	for _, r := range reasons {
		if strings.Contains(r, sub) {
			return true
		}
	}
	return false
}

func TestRunSharedHelperChange(t *testing.T) {
	dir, base := newApp(t)
	head := commitFiles(t, dir, map[string]*string{
		"app/main.go":  str(strings.Replace(appSrc, "const saltRounds = 3", "const saltRounds = 4", 1)),
		"docs/note.md": str("changed\n"),
	})
	sc, err := Run(Options{Repo: dir, Base: base, Head: head, Root: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Base != base || sc.Head != head || len(sc.Base) != 40 || sc.Root != "app" || sc.Version != model.FormatVersion {
		t.Errorf("header: %+v", sc)
	}
	if !strings.Contains(sc.Method, "depth 2") || !strings.Contains(sc.Method, "misses") {
		t.Errorf("method: %q", sc.Method)
	}
	if len(sc.Routes) != 6 {
		t.Errorf("want 6 routes, got %d: %+v", len(sc.Routes), sc.Routes)
	}
	if !reflect.DeepEqual(sc.ChangedFuncs, []string{"hashSecret"}) {
		t.Errorf("changed funcs: %v", sc.ChangedFuncs)
	}
	if len(sc.ChangedFiles) != 1 || sc.ChangedFiles[0].Path != "app/main.go" ||
		sc.ChangedFiles[0].Hunks[0].NewStart != 8 || !strings.Contains(sc.ChangedFiles[0].Hunks[0].Text, "+const saltRounds = 4") {
		t.Errorf("changed files: %+v", sc.ChangedFiles)
	}

	login := find(sc.Changed, "POST", "/login")
	if login == nil || !anyContains(login.Reasons, "handler (Server).handleLogin calls changed function hashSecret (depth 1)") {
		t.Fatalf("login: %+v", login)
	}
	if !reflect.DeepEqual(login.ChangedFuncs, []string{"hashSecret"}) {
		t.Errorf("login changed funcs: %v", login.ChangedFuncs)
	}
	for _, r := range [][2]string{{"POST", "/books"}, {"DELETE", "/books/{id}"}} {
		im := find(sc.Changed, r[0], r[1])
		if im == nil {
			t.Fatalf("%v not in Changed: %+v", r, sc.Changed)
		}
		want := "middleware (Server).requireAuth reaches changed function hashSecret (depth 2): every request on this route runs it"
		if !anyContains(im.Reasons, want) {
			t.Errorf("%v reasons: %v", r, im.Reasons)
		}
		if !reflect.DeepEqual(im.Middleware, []string{"(Server).requireAuth"}) || im.Handler == "" {
			t.Errorf("%v route fields: %+v", r, im.Route)
		}
	}
	if len(sc.Changed) != 3 {
		t.Errorf("changed routes: %d", len(sc.Changed))
	}

	list := find(sc.Affected, "GET", "/books")
	if list == nil || !anyContains(list.Reasons, "shares function (Store).List with changed route POST /books") {
		t.Fatalf("GET /books: %+v", list)
	}
	if find(sc.Affected, "GET", "/health") != nil || find(sc.Changed, "GET", "/health") != nil {
		t.Error("GET /health shares nothing and must not be listed")
	}
	for _, im := range sc.Affected {
		if anyContains(im.Reasons, "writeJSON") {
			t.Errorf("writeJSON is reached by most routes and must not be a shared-function reason: %v", im.Reasons)
		}
		if len(im.Reasons) == 0 || len(im.Reasons) > 5 {
			t.Errorf("%s reasons: %v", im.Path, im.Reasons)
		}
	}
	if !anyContains(sc.Notes, "diff touches files outside app: 1") {
		t.Errorf("notes: %v", sc.Notes)
	}
}

func TestRunSharedVariable(t *testing.T) {
	dir, base := newApp(t)
	head := commitFiles(t, dir, map[string]*string{
		"app/main.go": str(strings.Replace(appSrc, "\tsessions[tok] = true\n", "\tsessions[tok] = len(tok) > 0\n", 1)),
	})
	sc, err := Run(Options{Repo: dir, Base: base, Head: head, Root: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sc.ChangedFuncs, []string{"markSession"}) {
		t.Fatalf("changed funcs: %v", sc.ChangedFuncs)
	}
	if find(sc.Changed, "POST", "/login") == nil || len(sc.Changed) != 1 {
		t.Errorf("changed: %+v", sc.Changed)
	}
	me := find(sc.Affected, "GET", "/me")
	if me == nil || !anyContains(me.Reasons, "shares package variable sessions with changed function markSession") {
		t.Fatalf("GET /me: %+v", me)
	}
	if len(sc.Notes) != 0 {
		t.Errorf("notes: %v", sc.Notes)
	}
}

func TestRunDeletionOnlyHunk(t *testing.T) {
	dir, base := newApp(t)
	head := commitFiles(t, dir, map[string]*string{
		"app/main.go": str(strings.Replace(appSrc, "\tsessions[tok] = true\n", "", 1)),
	})
	sc, err := Run(Options{Repo: dir, Base: base, Head: head, Root: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.ChangedFiles) != 1 || sc.ChangedFiles[0].Hunks[0].NewLines != 0 {
		t.Fatalf("want a deletion-only hunk: %+v", sc.ChangedFiles)
	}
	if !reflect.DeepEqual(sc.ChangedFuncs, []string{"markSession"}) {
		t.Errorf("changed funcs: %v", sc.ChangedFuncs)
	}
}

func TestRunNoGoChangesAndDeletedFile(t *testing.T) {
	dir, base := newApp(t)
	head := commitFiles(t, dir, map[string]*string{"app/README.txt": str("x\n"), "app/extra.go": str("package main\n\nfunc extra() {}\nfunc (s *Store) Count() int { return 0 }\n")})
	sc, err := Run(Options{Repo: dir, Base: base, Head: head, Root: "app"})
	if err != nil {
		t.Fatal(err)
	}
	// extra.go is a new file: all of its functions are in the diff.
	if !reflect.DeepEqual(sc.ChangedFuncs, []string{"(Store).Count", "extra"}) {
		t.Errorf("changed funcs: %v", sc.ChangedFuncs)
	}
	if len(sc.Changed) != 0 || len(sc.Affected) != 0 {
		t.Errorf("unreachable functions must not change routes: %+v %+v", sc.Changed, sc.Affected)
	}

	head2 := commitFiles(t, dir, map[string]*string{"app/extra.go": nil})
	sc, err = Run(Options{Repo: dir, Base: head, Head: head2, Root: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.ChangedFuncs) != 0 || !anyContains(sc.Notes, "app/extra.go was deleted; it declared extra, (Store).Count") {
		t.Errorf("deleted file: funcs %v notes %v", sc.ChangedFuncs, sc.Notes)
	}

	head3 := commitFiles(t, dir, map[string]*string{"app/README.txt": str("y\n")})
	sc, err = Run(Options{Repo: dir, Base: head2, Head: head3, Root: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if !anyContains(sc.Notes, "no Go files changed under app") {
		t.Errorf("notes: %v", sc.Notes)
	}
	if sc.Changed == nil || sc.Affected == nil || sc.ChangedFuncs == nil || sc.ChangedFiles == nil {
		t.Error("lists must be non-nil so the JSON has [] and not null")
	}
}

func TestRunMissingRoot(t *testing.T) {
	dir, base := newApp(t)
	head := commitFiles(t, dir, map[string]*string{"docs/note.md": str("2\n")})
	sc, err := Run(Options{Repo: dir, Base: base, Head: head, Root: "nowhere"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.Routes) != 0 || !anyContains(sc.Notes, "nowhere does not exist in the head commit") {
		t.Errorf("%+v", sc)
	}
}

func TestRunErrors(t *testing.T) {
	dir, base := newApp(t)
	for name, o := range map[string]Options{
		"bad base":    {Repo: dir, Base: "nope", Head: base, Root: "app"},
		"bad head":    {Repo: dir, Base: base, Head: "nope", Root: "app"},
		"flag as rev": {Repo: dir, Base: "--output=x", Head: base, Root: "app"},
		"root escape": {Repo: dir, Base: base, Head: base, Root: "../x"},
		"abs root":    {Repo: dir, Base: base, Head: base, Root: "/etc"},
	} {
		if _, err := Run(o); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestRunLeavesWorkingTreeAlone(t *testing.T) {
	dir, base := newApp(t)
	head := commitFiles(t, dir, map[string]*string{"app/main.go": str(strings.Replace(appSrc, "= 3", "= 5", 1))})
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{Repo: dir, Base: base, Head: head, Root: "app"}); err != nil {
		t.Fatal(err)
	}
	if got := gitT(t, dir, "status", "--porcelain"); got != "?? untracked.txt" {
		t.Errorf("status: %q", got)
	}
	if got := gitT(t, dir, "worktree", "list"); strings.Count(got, "\n") != 0 {
		t.Errorf("worktrees: %q", got)
	}
}

func TestCrossPackageEdges(t *testing.T) {
	src := map[string]string{
		"main.go": `package main

import (
	"net/http"

	"example.com/m/internal/store"
)

func main() {
	http.HandleFunc("GET /a", handleA)
}

func handleA(w http.ResponseWriter, r *http.Request) {
	store.Save(r.URL.Path)
}
`,
		"internal/store/store.go": "package store\n\nfunc Save(s string) { inner(s) }\nfunc inner(s string) {}\nfunc Unused() {}\n",
	}
	dir := writeTree(t, src)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prog, err := load(loadConfig{dir: dir, prefix: "x", module: "example.com/m", modBase: "."})
	if err != nil {
		t.Fatal(err)
	}
	handler := prog.byKey["handleA"]
	if handler == nil {
		t.Fatal("handleA not indexed")
	}
	r := reach([]*function{handler}, 2)
	var keys []string
	for f := range r {
		keys = append(keys, f.key)
	}
	want := map[string]bool{"handleA": true, "internal/store.Save": true, "internal/store.inner": true}
	if len(keys) != 3 {
		t.Fatalf("reach: %v", keys)
	}
	for _, k := range keys {
		if !want[k] {
			t.Errorf("unexpected %s", k)
		}
	}
	if _, ok := reach([]*function{handler}, 1)[prog.byKey["internal/store.inner"]]; ok {
		t.Error("inner is two calls away and must not be in a depth 1 reach")
	}
}

func TestRunUbiquitousFunctionStillChangesRoutes(t *testing.T) {
	dir, base := newApp(t)
	head := commitFiles(t, dir, map[string]*string{
		"app/main.go": str(strings.Replace(appSrc, "_ = json.NewEncoder(w).Encode(v)", "_ = json.NewEncoder(w).Encode([]any{v})", 1)),
	})
	sc, err := Run(Options{Repo: dir, Base: base, Head: head, Root: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sc.ChangedFuncs, []string{"writeJSON"}) {
		t.Fatalf("changed funcs: %v", sc.ChangedFuncs)
	}
	if len(sc.Changed) != 5 || find(sc.Changed, "GET", "/health") != nil {
		t.Errorf("every route that reaches writeJSON must be Changed: %+v", sc.Changed)
	}
	if !strings.Contains(sc.Method, "more than half of all routes") {
		t.Errorf("method must mention the rule: %q", sc.Method)
	}
}
