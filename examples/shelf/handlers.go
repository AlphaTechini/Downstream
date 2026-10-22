package shelf

import (
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	usernameRe = regexp.MustCompile(`^[a-z0-9_]{3,40}$`)
	tagRe      = regexp.MustCompile(`^[a-z0-9_-]{1,30}$`)
)

const (
	maxListed = 50
	maxTags   = 10
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decode(w, r, &in) {
		return
	}
	if !usernameRe.MatchString(in.Username) {
		writeError(w, http.StatusBadRequest, "username must match [a-z0-9_]{3,40}")
		return
	}
	if len(in.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	u, ok := s.store.addUser(in.Username, hashPassword(in.Password))
	if !ok {
		writeError(w, http.StatusConflict, "username is taken")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": u.ID, "username": u.Username})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decode(w, r, &in) {
		return
	}
	u, found := s.store.userByName(in.Username)
	stored := dummyPassword
	if found {
		stored = u.Password
	}
	if !checkPassword(stored, in.Password) || !found {
		writeError(w, http.StatusUnauthorized, "bad username or password")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": s.issueToken(u.ID)})
}

func (s *Server) handleListBooks(w http.ResponseWriter, r *http.Request) {
	books := s.store.listBooks(r.URL.Query().Get("tag"), maxListed)
	writeJSON(w, http.StatusOK, map[string]any{"books": books})
}

func (s *Server) handleGetBook(w http.ResponseWriter, r *http.Request) {
	b, ok := s.store.book(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such book")
		return
	}
	writeJSON(w, http.StatusOK, b)
}

type bookInput struct {
	Title  string   `json:"title"`
	Author string   `json:"author"`
	Tags   []string `json:"tags"`
}

func (s *Server) handleCreateBook(w http.ResponseWriter, r *http.Request) {
	var in bookInput
	if !decode(w, r, &in) {
		return
	}
	in.Title, in.Author = strings.TrimSpace(in.Title), strings.TrimSpace(in.Author)
	if n := utf8.RuneCountInString(in.Title); n < 1 || n > 200 {
		writeError(w, http.StatusBadRequest, "title must be 1 to 200 characters")
		return
	}
	if n := utf8.RuneCountInString(in.Author); n < 1 || n > 120 {
		writeError(w, http.StatusBadRequest, "author must be 1 to 120 characters")
		return
	}
	if len(in.Tags) > maxTags {
		writeError(w, http.StatusBadRequest, "at most 10 tags")
		return
	}
	tags := make([]string, 0, len(in.Tags))
	for _, t := range in.Tags {
		if !tagRe.MatchString(t) {
			writeError(w, http.StatusBadRequest, "tags must match [a-z0-9_-]{1,30}")
			return
		}
		tags = append(tags, t)
	}
	writeJSON(w, http.StatusCreated, s.store.addBook(userID(r), in.Title, in.Author, tags))
}

type reviewInput struct {
	Rating int    `json:"rating"`
	Text   string `json:"text"`
}

func (s *Server) handleCreateReview(w http.ResponseWriter, r *http.Request) {
	var in reviewInput
	if !decode(w, r, &in) {
		return
	}
	if in.Rating < 1 || in.Rating > 5 {
		writeError(w, http.StatusBadRequest, "rating must be 1 to 5")
		return
	}
	if utf8.RuneCountInString(in.Text) > 2000 {
		writeError(w, http.StatusBadRequest, "text must be at most 2000 characters")
		return
	}
	rev, ok := s.store.addReview(r.PathValue("id"), userID(r), in.Rating, in.Text)
	if !ok {
		writeError(w, http.StatusNotFound, "no such book")
		return
	}
	writeJSON(w, http.StatusCreated, rev)
}

func (s *Server) handleListReviews(w http.ResponseWriter, r *http.Request) {
	revs, ok := s.store.bookReviews(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such book")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": revs})
}

func (s *Server) handleShelfAdd(w http.ResponseWriter, r *http.Request) {
	ids, ok := s.store.shelfAdd(userID(r), r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such book")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shelf": ids})
}

func (s *Server) handleShelfList(w http.ResponseWriter, r *http.Request) {
	_, books := s.store.shelf(userID(r))
	if books == nil {
		books = []*Book{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"books": books})
}

func (s *Server) handleShelfRemove(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"shelf": s.store.shelfRemove(userID(r), r.PathValue("id"))})
}
