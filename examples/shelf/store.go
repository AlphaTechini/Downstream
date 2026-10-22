package shelf

import (
	"strconv"
	"sync"
	"time"
)

// User is a registered reader. Password holds salt followed by hashSecret(password, salt).
type User struct {
	ID       string
	Username string
	Password []byte
}

// Book is one entry in the shared reading list.
type Book struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Author    string    `json:"author"`
	Tags      []string  `json:"tags"`
	AddedBy   string    `json:"added_by"`
	CreatedAt time.Time `json:"created_at"`
}

// Review is a rating and a short text for one book.
type Review struct {
	ID        string    `json:"id"`
	BookID    string    `json:"book_id"`
	UserID    string    `json:"user_id"`
	Rating    int       `json:"rating"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// tokenRecord is what the store keeps for an API token. The secret half of the
// token is never stored, only its salted hash.
type tokenRecord struct {
	UserID string
	Salt   []byte
	Hash   []byte
}

// store is the in-memory data. One RWMutex guards all of it.
type store struct {
	mu      sync.RWMutex
	nextID  int
	users   map[string]*User // by ID
	byName  map[string]*User
	books   []*Book // oldest first
	bookIdx map[string]*Book
	reviews map[string][]*Review // by book ID, oldest first
	shelves map[string][]string  // user ID -> book IDs in the order added
	// API tokens are hashed at rest like passwords so a leaked store does not leak live tokens.
	tokens map[string]tokenRecord // by token ID
}

func newStore() *store {
	return &store{
		users:   map[string]*User{},
		byName:  map[string]*User{},
		bookIdx: map[string]*Book{},
		reviews: map[string][]*Review{},
		shelves: map[string][]string{},
		tokens:  map[string]tokenRecord{},
	}
}

// id returns the next short decimal ID. The caller holds the write lock.
func (s *store) id() string {
	s.nextID++
	return strconv.Itoa(s.nextID)
}

func (s *store) addUser(username string, password []byte) (*User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, taken := s.byName[username]; taken {
		return nil, false
	}
	u := &User{ID: s.id(), Username: username, Password: password}
	s.users[u.ID] = u
	s.byName[username] = u
	return u, true
}

func (s *store) userByName(username string) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.byName[username]
	return u, ok
}

func (s *store) putToken(tokenID string, rec tokenRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[tokenID] = rec
}

func (s *store) token(tokenID string) (tokenRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.tokens[tokenID]
	return rec, ok
}

func (s *store) addBook(userID, title, author string, tags []string) *Book {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := &Book{ID: s.id(), Title: title, Author: author, Tags: tags, AddedBy: userID, CreatedAt: time.Now().UTC()}
	s.books = append(s.books, b)
	s.bookIdx[b.ID] = b
	return b
}

func (s *store) book(id string) (*Book, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.bookIdx[id]
	return b, ok
}

// listBooks returns up to limit books, newest first, optionally only those with tag.
func (s *store) listBooks(tag string, limit int) []*Book {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Book, 0, limit)
	for i := len(s.books) - 1; i >= 0 && len(out) < limit; i-- {
		b := s.books[i]
		if tag == "" || hasTag(b.Tags, tag) {
			out = append(out, b)
		}
	}
	return out
}

func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

func (s *store) addReview(bookID, userID string, rating int, text string) (*Review, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.bookIdx[bookID]; !ok {
		return nil, false
	}
	r := &Review{ID: s.id(), BookID: bookID, UserID: userID, Rating: rating, Text: text, CreatedAt: time.Now().UTC()}
	s.reviews[bookID] = append(s.reviews[bookID], r)
	return r, true
}

func (s *store) bookReviews(bookID string) ([]*Review, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.bookIdx[bookID]; !ok {
		return nil, false
	}
	return append([]*Review(nil), s.reviews[bookID]...), true
}

// shelfAdd puts a book on the user's shelf once. It reports false when the book does not exist.
func (s *store) shelfAdd(userID, bookID string) ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.bookIdx[bookID]; !ok {
		return nil, false
	}
	shelf := s.shelves[userID]
	if !hasTag(shelf, bookID) {
		shelf = append(shelf, bookID)
		s.shelves[userID] = shelf
	}
	return append([]string{}, shelf...), true
}

func (s *store) shelfRemove(userID, bookID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]string, 0, len(s.shelves[userID]))
	for _, id := range s.shelves[userID] {
		if id != bookID {
			kept = append(kept, id)
		}
	}
	s.shelves[userID] = kept
	return append([]string{}, kept...)
}

func (s *store) shelf(userID string) (ids []string, books []*Book) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids = append([]string{}, s.shelves[userID]...)
	for _, id := range ids {
		if b, ok := s.bookIdx[id]; ok {
			books = append(books, b)
		}
	}
	return ids, books
}
