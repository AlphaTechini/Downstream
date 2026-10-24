package runner

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"downstream/engine/target"
)

func TestStaticHandler(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(root, "index.html"), []byte("<p>app</p>"), 0o644))
	must(os.MkdirAll(filepath.Join(root, "assets"), 0o755))
	must(os.WriteFile(filepath.Join(root, "assets", "app-AbCd1234.js"), []byte("console.log(1)"), 0o644))
	must(os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	must(os.WriteFile(filepath.Join(root, "docs", "index.html"), []byte("<p>docs</p>"), 0o644))
	outside := filepath.Join(t.TempDir(), "secret.txt")
	must(os.WriteFile(outside, []byte("secret"), 0o644))
	must(os.Symlink(outside, filepath.Join(root, "leak.txt")))

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "api "+r.URL.Path) }))
	defer api.Close()
	apiURL, _ := url.Parse(api.URL)
	srv := httptest.NewServer(staticHandler(root, apiURL))
	defer srv.Close()

	for _, tc := range []struct {
		path, body string
		status     int
	}{
		{"/", "<p>app</p>", 200},
		{"/assets/app-AbCd1234.js", "console.log(1)", 200},
		{"/docs/", "<p>docs</p>", 200},
		{"/reports/42", "<p>app</p>", 200},       // client-side route
		{"/assets/missing.js", "", 404},          // broken asset reference
		{"/leak.txt", "", 404},                   // symlink out of the build
		{"/../../etc/passwd", "<p>app</p>", 200}, // cleaned to a path inside root, so the app shell
		{"/api/items", "api /api/items", 200},    // proxied to the backend
	} {
		resp, err := http.Get(srv.URL + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.status || (tc.body != "" && string(b) != tc.body) {
			t.Errorf("%s: got %d %q, want %d %q", tc.path, resp.StatusCode, b, tc.status, tc.body)
		}
	}
}

func TestFrontendChanges(t *testing.T) {
	files := []string{"web/src/App.tsx", "web/dist/index.js", "web/package-lock.json", "api/main.go", "web/README.md", "templates/page.gohtml"}
	static := target.Config{Frontend: target.Frontend{Dir: "web", Build: "npm run build", Out: "web/dist"}}
	got := frontendChanges(static, files)
	want := []string{"web/src/App.tsx", "web/package-lock.json"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("static: got %v, want %v", got, want)
	}
	server := target.Config{Build: "go build -o {out} ./cmd/app"}
	got = frontendChanges(server, files)
	if len(got) != 4 || got[3] != "templates/page.gohtml" {
		t.Fatalf("server: got %v", got)
	}
}

func TestLinkTree(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "nm")
	if err := os.MkdirAll(filepath.Join(src, "pkg", "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "pkg", "lib", "a.js"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../pkg/lib/a.js", filepath.Join(src, "pkg", "bin")); err != nil {
		t.Fatal(err)
	}
	if err := linkTree(src, dst); err != nil {
		t.Fatal(err)
	}
	a, _ := os.Stat(filepath.Join(src, "pkg", "lib", "a.js"))
	b, err := os.Stat(filepath.Join(dst, "pkg", "lib", "a.js"))
	if err != nil || !os.SameFile(a, b) {
		t.Fatalf("a.js is not a hard link: %v", err)
	}
	if l, err := os.Readlink(filepath.Join(dst, "pkg", "bin")); err != nil || l != "../pkg/lib/a.js" {
		t.Fatalf("symlink = %q, %v", l, err)
	}
}
