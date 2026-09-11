package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"
)

type session struct {
	userID  int32
	expires time.Time
}
type Sessions struct {
	mu      sync.Mutex
	entries map[[32]byte]session
	secure  bool
	now     func() time.Time
}

func NewSessions(secure bool) *Sessions {
	return &Sessions{entries: make(map[[32]byte]session), secure: secure, now: time.Now}
}
func RandomToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
func (s *Sessions) CookieName() string {
	if s.secure {
		return "__Host-club_session"
	}
	return "club_session"
}
func (s *Sessions) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: s.CookieName(), Value: value, Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge}
}
func (s *Sessions) Create(w http.ResponseWriter, r *http.Request, userID int32) error {
	token, err := RandomToken()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, entry := range s.entries {
		if !now.Before(entry.expires) {
			delete(s.entries, key)
		}
	}
	if len(s.entries) >= 10000 {
		return errors.New("session capacity reached")
	}
	// Rotate rather than reuse a supplied session identifier.
	if old, e := r.Cookie(s.CookieName()); e == nil {
		delete(s.entries, sha256.Sum256([]byte(old.Value)))
	}
	s.entries[sha256.Sum256([]byte(token))] = session{userID: userID, expires: now.Add(12 * time.Hour)}
	http.SetCookie(w, s.cookie(token, 12*60*60))
	return nil
}
func (s *Sessions) UserID(r *http.Request) (int32, bool) {
	cookie, err := r.Cookie(s.CookieName())
	if err != nil {
		return 0, false
	}
	key := sha256.Sum256([]byte(cookie.Value))
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok {
		return 0, false
	}
	if !s.now().Before(entry.expires) {
		delete(s.entries, key)
		return 0, false
	}
	return entry.userID, true
}
func (s *Sessions) Delete(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(s.CookieName()); err == nil {
		s.mu.Lock()
		delete(s.entries, sha256.Sum256([]byte(cookie.Value)))
		s.mu.Unlock()
	}
	http.SetCookie(w, s.cookie("", -1))
}

type userKey struct{}

func UserID(ctx context.Context) (int32, bool) { id, ok := ctx.Value(userKey{}).(int32); return id, ok }

// Middleware loads and revalidates a session; it grants no administrative rights.
func (s *Sessions) Middleware(service *Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := s.UserID(r); ok {
			if service.SessionUser(r.Context(), id) {
				r = r.WithContext(context.WithValue(r.Context(), userKey{}, id))
			} else {
				s.Delete(w, r)
			}
		}
		next.ServeHTTP(w, r)
	})
}
