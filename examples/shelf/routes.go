package shelf

import "net/http"

// Routes registers every route in one place.
func Routes(s *Server) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /users", s.handleCreateUser)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("GET /books", s.handleListBooks)
	mux.HandleFunc("GET /books/{id}", s.handleGetBook)
	mux.Handle("POST /books", s.requireAuth(http.HandlerFunc(s.handleCreateBook)))
	mux.Handle("POST /books/{id}/reviews", s.requireAuth(http.HandlerFunc(s.handleCreateReview)))
	mux.HandleFunc("GET /books/{id}/reviews", s.handleListReviews)
	mux.Handle("PUT /me/shelf/{id}", s.requireAuth(http.HandlerFunc(s.handleShelfAdd)))
	mux.Handle("GET /me/shelf", s.requireAuth(http.HandlerFunc(s.handleShelfList)))
	mux.Handle("DELETE /me/shelf/{id}", s.requireAuth(http.HandlerFunc(s.handleShelfRemove)))
	return mux
}
