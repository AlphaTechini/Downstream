// Package shelf is a small reading-list JSON API with an in-memory store. It is the
// demo target for Downstream: a realistic app whose authenticated routes all pay
// for the same token check.
package shelf

import (
	"encoding/json"
	"net/http"
)

// Server holds the store and serves the API.
type Server struct {
	store *store
}

// NewServer returns a server with an empty store.
func NewServer() *Server {
	return &Server{store: newStore()}
}

// Handler returns the full route table.
func (s *Server) Handler() http.Handler {
	return Routes(s)
}

const maxBody = 64 << 10

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decode reads a JSON request body into dst and answers 400 itself on failure.
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}
