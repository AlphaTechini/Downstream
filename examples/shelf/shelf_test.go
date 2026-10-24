package shelf

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"downstream/engine/model"
)

type client struct {
	t   *testing.T
	srv *httptest.Server
}

func newClient(t *testing.T) *client {
	t.Helper()
	srv := httptest.NewServer(NewServer().Handler())
	t.Cleanup(srv.Close)
	return &client{t: t, srv: srv}
}

// do sends a request and returns the status and decoded JSON object.
func (c *client) do(method, path, token string, body any) (int, map[string]any) {
	c.t.Helper()
	var rd *bytes.Reader
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			c.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.srv.URL+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (c *client) want(status, got int, out map[string]any) {
	c.t.Helper()
	if status != got {
		c.t.Fatalf("status = %d, want %d (body %v)", got, status, out)
	}
}

func (c *client) login(name string) string {
	c.t.Helper()
	cred := map[string]string{"username": name, "password": "correct-horse"}
	st, out := c.do("POST", "/users", "", cred)
	c.want(201, st, out)
	st, out = c.do("POST", "/login", "", cred)
	c.want(200, st, out)
	return out["token"].(string)
}

func TestHealth(t *testing.T) {
	c := newClient(t)
	st, out := c.do("GET", "/health", "", nil)
	c.want(200, st, out)
	if out["ok"] != true {
		t.Fatalf("body = %v", out)
	}
}

func TestRegisterAndLogin(t *testing.T) {
	c := newClient(t)
	cred := map[string]string{"username": "ada_1", "password": "correct-horse"}
	st, out := c.do("POST", "/users", "", cred)
	c.want(201, st, out)
	if out["username"] != "ada_1" || out["id"] == "" {
		t.Fatalf("body = %v", out)
	}
	st, out = c.do("POST", "/users", "", cred)
	c.want(409, st, out)

	for name, bad := range map[string]map[string]string{
		"short username": {"username": "ab", "password": "correct-horse"},
		"upper case":     {"username": "ADA_2", "password": "correct-horse"},
		"short password": {"username": "ada_3", "password": "short"},
	} {
		st, out := c.do("POST", "/users", "", bad)
		if st != 400 || out["error"] == nil {
			t.Errorf("%s: status %d body %v", name, st, out)
		}
	}
	st, out = c.do("POST", "/users", "", []byte("{not json"))
	c.want(400, st, out)

	st, out = c.do("POST", "/login", "", cred)
	c.want(200, st, out)
	if tok, _ := out["token"].(string); !strings.Contains(tok, ".") {
		t.Fatalf("token = %v", out["token"])
	}
	st, out = c.do("POST", "/login", "", map[string]string{"username": "ada_1", "password": "wrong-password"})
	c.want(401, st, out)
	st, out = c.do("POST", "/login", "", map[string]string{"username": "nobody", "password": "correct-horse"})
	c.want(401, st, out)
}

func TestAuthFailures(t *testing.T) {
	c := newClient(t)
	good := c.login("ada_1")
	tokenID, _, _ := strings.Cut(good, ".")
	for name, tok := range map[string]string{
		"none":          "",
		"garbage":       "nonsense",
		"wrong secret":  tokenID + ".deadbeef",
		"unknown token": "0000.deadbeef",
	} {
		st, out := c.do("GET", "/me/shelf", tok, nil)
		if st != 401 {
			t.Errorf("%s: status %d body %v", name, st, out)
		}
	}
	st, out := c.do("POST", "/books", "", map[string]any{"title": "x", "author": "y"})
	c.want(401, st, out)
	st, out = c.do("GET", "/me/shelf", good, nil)
	c.want(200, st, out)
}

func TestTokenSecretNotStored(t *testing.T) {
	s := NewServer()
	tok := s.issueToken("1")
	id, secret, _ := strings.Cut(tok, ".")
	rec, ok := s.store.token(id)
	if !ok {
		t.Fatal("token not stored")
	}
	if bytes.Contains(rec.Hash, []byte(secret)) || string(rec.Hash) == secret {
		t.Fatal("secret stored in clear")
	}
	if uid, ok := s.verifyToken(tok); !ok || uid != "1" {
		t.Fatalf("verify = %q, %v", uid, ok)
	}
}

func TestBooks(t *testing.T) {
	c := newClient(t)
	tok := c.login("ada_1")

	st, out := c.do("POST", "/books", tok, map[string]any{"title": "", "author": "x"})
	c.want(400, st, out)
	st, out = c.do("POST", "/books", tok, map[string]any{"title": "t", "author": "a", "tags": []string{"Bad Tag"}})
	c.want(400, st, out)

	var ids []string
	for i, tags := range [][]string{{"go"}, {"go", "web"}, {"web"}} {
		st, out = c.do("POST", "/books", tok, map[string]any{"title": "Book " + strconv.Itoa(i), "author": "A", "tags": tags})
		c.want(201, st, out)
		ids = append(ids, out["id"].(string))
	}

	st, out = c.do("GET", "/books", "", nil)
	c.want(200, st, out)
	books := out["books"].([]any)
	if len(books) != 3 || books[0].(map[string]any)["id"] != ids[2] {
		t.Fatalf("not newest first: %v", books)
	}
	st, out = c.do("GET", "/books?tag=go", "", nil)
	c.want(200, st, out)
	if n := len(out["books"].([]any)); n != 2 {
		t.Fatalf("tag go: %d books", n)
	}
	st, out = c.do("GET", "/books/"+ids[1], "", nil)
	c.want(200, st, out)
	if out["title"] != "Book 1" {
		t.Fatalf("book = %v", out)
	}
	st, out = c.do("GET", "/books/999", "", nil)
	c.want(404, st, out)
}

func TestBookListCapped(t *testing.T) {
	c := newClient(t)
	tok := c.login("ada_1")
	for i := 0; i < maxListed+5; i++ {
		st, out := c.do("POST", "/books", tok, map[string]any{"title": "b", "author": "a"})
		c.want(201, st, out)
	}
	_, out := c.do("GET", "/books", "", nil)
	if n := len(out["books"].([]any)); n != maxListed {
		t.Fatalf("listed %d, want %d", n, maxListed)
	}
}

func TestReviews(t *testing.T) {
	c := newClient(t)
	tok := c.login("ada_1")
	_, book := c.do("POST", "/books", tok, map[string]any{"title": "t", "author": "a"})
	id := book["id"].(string)

	st, out := c.do("POST", "/books/"+id+"/reviews", "", map[string]any{"rating": 5})
	c.want(401, st, out)
	st, out = c.do("POST", "/books/"+id+"/reviews", tok, map[string]any{"rating": 6})
	c.want(400, st, out)
	st, out = c.do("POST", "/books/999/reviews", tok, map[string]any{"rating": 3})
	c.want(404, st, out)
	st, out = c.do("POST", "/books/"+id+"/reviews", tok, map[string]any{"rating": 4, "text": "good"})
	c.want(201, st, out)
	if out["rating"] != float64(4) || out["id"] == "" {
		t.Fatalf("review = %v", out)
	}
	st, out = c.do("GET", "/books/"+id+"/reviews", "", nil)
	c.want(200, st, out)
	if n := len(out["reviews"].([]any)); n != 1 {
		t.Fatalf("%d reviews", n)
	}
	st, out = c.do("GET", "/books/999/reviews", "", nil)
	c.want(404, st, out)
}

func TestShelf(t *testing.T) {
	c := newClient(t)
	tok := c.login("ada_1")
	other := c.login("bob_1")
	_, book := c.do("POST", "/books", tok, map[string]any{"title": "t", "author": "a"})
	id := book["id"].(string)

	st, out := c.do("GET", "/me/shelf", tok, nil)
	c.want(200, st, out)
	if n := len(out["books"].([]any)); n != 0 {
		t.Fatalf("new shelf has %d books", n)
	}
	st, out = c.do("PUT", "/me/shelf/999", tok, nil)
	c.want(404, st, out)
	for i := 0; i < 2; i++ { // second add is a no-op
		st, out = c.do("PUT", "/me/shelf/"+id, tok, nil)
		c.want(200, st, out)
		if shelf := out["shelf"].([]any); len(shelf) != 1 || shelf[0] != id {
			t.Fatalf("shelf = %v", shelf)
		}
	}
	st, out = c.do("GET", "/me/shelf", tok, nil)
	c.want(200, st, out)
	if n := len(out["books"].([]any)); n != 1 {
		t.Fatalf("shelf has %d books", n)
	}
	st, out = c.do("GET", "/me/shelf", other, nil)
	c.want(200, st, out)
	if n := len(out["books"].([]any)); n != 0 {
		t.Fatalf("other user's shelf has %d books", n)
	}
	st, out = c.do("DELETE", "/me/shelf/"+id, tok, nil)
	c.want(200, st, out)
	if shelf := out["shelf"].([]any); len(shelf) != 0 {
		t.Fatalf("shelf = %v", shelf)
	}
}

// TestExampleJourneys replays journeys.example.json against a fresh server. The
// substitution and JSON pointer lookup are the minimum the file needs; the engine's
// runner does the real work.
func TestExampleJourneys(t *testing.T) {
	raw, err := os.ReadFile("journeys.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var jf model.JourneyFile
	if err := json.Unmarshal(raw, &jf); err != nil {
		t.Fatal(err)
	}
	if jf.Version != 1 || len(jf.Journeys) != 5 {
		t.Fatalf("version %d, %d journeys", jf.Version, len(jf.Journeys))
	}
	c := newClient(t)
	var uniq atomic.Int64
	// Run the set twice: the second round proves {{uniq}} keeps usernames apart on a shared store.
	for round := 0; round < 2; round++ {
		for _, j := range jf.Journeys {
			vars := map[string]string{"uniq": "t" + strconv.FormatInt(uniq.Add(1), 10)}
			for _, st := range j.Steps {
				fill := func(s string) string {
					for k, v := range vars {
						s = strings.ReplaceAll(s, "{{"+k+"}}", v)
					}
					return s
				}
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
					v, ok := pointer(doc, ptr)
					if !ok {
						t.Fatalf("%s/%s: nothing at %s", j.ID, st.Name, ptr)
					}
					vars[name] = v
				}
			}
		}
	}
}

// pointer looks up an RFC 6901 pointer of object keys and returns a scalar as text.
func pointer(doc any, ptr string) (string, bool) {
	cur := doc
	for _, part := range strings.Split(strings.TrimPrefix(ptr, "/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		if cur, ok = m[part]; !ok {
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
