package handlers

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type attemptWindow struct {
	count int
	until time.Time
}

// Local limiter: shared login/activation budget, 10 attempts/IP/15min,
// plus 120 attempts/minute globally; bounded memory and no forwarded-header trust.
type AttemptLimiter struct {
	mu                 sync.Mutex
	ips                map[string]attemptWindow
	global             attemptWindow
	now                func() time.Time
	perIP, globalLimit int
}

func NewAttemptLimiter() *AttemptLimiter {
	return &AttemptLimiter{ips: map[string]attemptWindow{}, now: time.Now, perIP: 10, globalLimit: 120}
}

// NewRegistrationSubmissionLimiter prepares a separate future public submission
// budget: 5 per IP / 15 minutes and 60 globally / minute, per process.
func NewRegistrationSubmissionLimiter() *AttemptLimiter {
	return &AttemptLimiter{ips: map[string]attemptWindow{}, now: time.Now, perIP: 5, globalLimit: 60}
}

// AllowRequest deliberately trusts only the direct peer, never forwarded headers.
func (l *AttemptLimiter) AllowRequest(r *http.Request) bool { return l.Allow(r.RemoteAddr) }
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
	if l.global.count >= l.globalLimit {
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
	if current.count >= l.perIP {
		return false
	}
	current.count++
	l.ips[ip] = current
	return true
}
func (h *AuthHandler) csrfName() string { return h.csrf.CookieName() }
func (h *AuthHandler) sensitive(next http.HandlerFunc) http.Handler {
	protected := h.csrf.Protect(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !h.limiter.Allow(r.RemoteAddr) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Retry-After", "900")
			http.Error(w, "Trop de tentatives. Réessayez plus tard.", http.StatusTooManyRequests)
			return
		}
		protected.ServeHTTP(w, r)
	})
}
