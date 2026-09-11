package handlers

import (
	"crypto/subtle"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/grapinou/club-core/internal/auth"
)

type attemptWindow struct {
	count int
	until time.Time
}

// Local limiter: shared login/activation budget, 10 attempts/IP/15min,
// plus 120 attempts/minute globally; bounded memory and no forwarded-header trust.
type AttemptLimiter struct {
	mu     sync.Mutex
	ips    map[string]attemptWindow
	global attemptWindow
	now    func() time.Time
}

func NewAttemptLimiter() *AttemptLimiter {
	return &AttemptLimiter{ips: map[string]attemptWindow{}, now: time.Now}
}
func (l *AttemptLimiter) Allow(remote string) bool {
	ip, _, err := net.SplitHostPort(remote)
	if err != nil {
		ip = remote
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if !now.Before(l.global.until) {
		l.global = attemptWindow{until: now.Add(time.Minute)}
	}
	if l.global.count >= 120 {
		return false
	}
	l.global.count++
	current, ok := l.ips[ip]
	if !ok || !now.Before(current.until) {
		for key, entry := range l.ips {
			if !now.Before(entry.until) {
				delete(l.ips, key)
			}
		}
		if len(l.ips) >= 10000 {
			return false
		}
		current = attemptWindow{until: now.Add(15 * time.Minute)}
	}
	if current.count >= 10 {
		return false
	}
	current.count++
	l.ips[ip] = current
	return true
}
func (h *AuthHandler) csrfName() string {
	if h.secure {
		return "__Host-club_csrf"
	}
	return "club_csrf"
}
func (h *AuthHandler) csrfToken(w http.ResponseWriter, r *http.Request) (string, error) {
	if c, err := r.Cookie(h.csrfName()); err == nil && len(c.Value) == 64 {
		return c.Value, nil
	}
	token, err := auth.RandomToken()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{Name: h.csrfName(), Value: token, Path: "/", HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteStrictMode, MaxAge: 3600})
	return token, nil
}
func (h *AuthHandler) sensitive(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'; form-action 'self'; base-uri 'self'")
		if r.Method == http.MethodPost {
			if !h.limiter.Allow(r.RemoteAddr) {
				w.Header().Set("Retry-After", "900")
				http.Error(w, "Trop de tentatives. Réessayez plus tard.", http.StatusTooManyRequests)
				return
			}
			if err := h.crossOrigin.Check(r); err != nil {
				http.Error(w, "Requête non autorisée.", http.StatusForbidden)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Requête invalide.", http.StatusBadRequest)
				return
			}
			c, err := r.Cookie(h.csrfName())
			token := r.PostForm.Get("csrf_token")
			if err != nil || len(token) != 64 || subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) != 1 {
				http.Error(w, "Requête non autorisée.", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	})
}
