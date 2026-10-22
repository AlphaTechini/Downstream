package shelf

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
)

type ctxKey struct{}

const saltLen = 16

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("shelf: no randomness: " + err.Error())
	}
	return b
}

// hashPassword returns salt followed by hashSecret(password, salt).
func hashPassword(password string) []byte {
	salt := randomBytes(saltLen)
	return append(salt, hashSecret(password, salt)...)
}

func checkPassword(stored []byte, password string) bool {
	if len(stored) <= saltLen {
		return false
	}
	salt, want := stored[:saltLen], stored[saltLen:]
	return subtle.ConstantTimeCompare(hashSecret(password, salt), want) == 1
}

// issueToken creates "<tokenID>.<secret>" and stores only the salted hash of the secret.
func (s *Server) issueToken(userID string) string {
	tokenID := hex.EncodeToString(randomBytes(8))
	secret := hex.EncodeToString(randomBytes(32))
	salt := randomBytes(saltLen)
	s.store.putToken(tokenID, tokenRecord{UserID: userID, Salt: salt, Hash: hashSecret(secret, salt)})
	return tokenID + "." + secret
}

// verifyToken returns the user ID a token belongs to.
func (s *Server) verifyToken(token string) (string, bool) {
	tokenID, secret, ok := strings.Cut(token, ".")
	if !ok || tokenID == "" || secret == "" {
		return "", false
	}
	rec, ok := s.store.token(tokenID)
	if !ok {
		return "", false
	}
	if subtle.ConstantTimeCompare(hashSecret(secret, rec.Salt), rec.Hash) != 1 {
		return "", false
	}
	return rec.UserID, true
}

// requireAuth answers 401 unless the request carries a valid bearer token, and
// otherwise puts the caller's user ID in the request context.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		userID, ok := s.verifyToken(strings.TrimSpace(token))
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, userID)))
	})
}

func userID(r *http.Request) string {
	id, _ := r.Context().Value(ctxKey{}).(string)
	return id
}

// dummyPassword lets a login for an unknown user cost the same as one for a known user.
var dummyPassword = bytes.Repeat([]byte{1}, saltLen+32)
