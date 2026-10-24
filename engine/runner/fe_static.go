package runner

import (
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// staticHandler serves a frontend build the way a single-page app host does: an
// existing file is served as is, a missing path with a file extension is a 404
// (so a broken asset reference shows up as missing), and any other path gets
// index.html so client-side routes load. With api set, /api/ is proxied to the
// backend on loopback.
func staticHandler(root string, api *url.URL) http.Handler {
	var proxy http.Handler
	if api != nil {
		proxy = httputil.NewSingleHostReverseProxy(api)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if proxy != nil && (r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/")) {
			proxy.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		clean := path.Clean("/" + r.URL.Path)
		if serveFile(w, r, root, clean) {
			return
		}
		if serveFile(w, r, root, path.Join(clean, "index.html")) {
			return
		}
		if path.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}
		if !serveFile(w, r, root, "/index.html") {
			http.NotFound(w, r)
		}
	})
}

// serveFile serves root+name when it is a regular file that resolves inside
// root, and reports whether it did. Symlinks in the build output that point
// elsewhere are treated as missing.
func serveFile(w http.ResponseWriter, r *http.Request, root, name string) bool {
	p := filepath.Join(root, filepath.FromSlash(name))
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return false
	}
	if rel, err := filepath.Rel(root, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	f, err := os.Open(real)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return false
	}
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
	return true
}

// staticServer is a running staticHandler on a loopback port.
type staticServer struct {
	srv  *http.Server
	port int
}

func startStatic(dir string, api *url.URL) (*staticServer, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(filepath.Join(root, "index.html")); err != nil || !st.Mode().IsRegular() {
		return nil, errors.New("the frontend build wrote no index.html in frontend_out")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &staticServer{
		srv:  &http.Server{Handler: staticHandler(root, api), ReadHeaderTimeout: 10 * time.Second},
		port: ln.Addr().(*net.TCPAddr).Port,
	}
	go func() { _ = s.srv.Serve(ln) }()
	return s, nil
}

func (s *staticServer) baseURL() string { return "http://127.0.0.1:" + strconv.Itoa(s.port) }

func (s *staticServer) stop() { _ = s.srv.Close() }
