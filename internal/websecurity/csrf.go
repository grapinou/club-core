// Package websecurity supplies the shared CSRF protection for all HTML mutations.
package websecurity

import (
	"context"
	"crypto/subtle"
	"net/http"

	"github.com/grapinou/club-core/internal/auth"
)

type CSRF struct {
	secure bool
	origin *http.CrossOriginProtection
}

func NewCSRF(secure bool) *CSRF {
	return &CSRF{secure: secure, origin: http.NewCrossOriginProtection()}
}
func (c *CSRF) CookieName() string {
	if c.secure {
		return "__Host-club_csrf"
	}
	return "club_csrf"
}
func (c *CSRF) Token(w http.ResponseWriter, r *http.Request) (string, error) {
	if cookie, err := r.Cookie(c.CookieName()); err == nil && len(cookie.Value) == 64 {
		return cookie.Value, nil
	}
	token, err := auth.RandomToken()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{Name: c.CookieName(), Value: token, Path: "/", HttpOnly: true, Secure: c.secure, SameSite: http.SameSiteStrictMode, MaxAge: 3600})
	return token, nil
}

type tokenKey struct{}

func Token(ctx context.Context) string { token, _ := ctx.Value(tokenKey{}).(string); return token }
func (c *CSRF) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'; form-action 'self'; base-uri 'self'")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if err := c.origin.Check(r); err != nil {
				http.Error(w, "Requête non autorisée.", 403)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Requête invalide.", 400)
				return
			}
			cookie, err := r.Cookie(c.CookieName())
			token := r.PostForm.Get("csrf_token")
			if err != nil || len(token) != 64 || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(token)) != 1 {
				http.Error(w, "Requête non autorisée.", 403)
				return
			}
		}
		token, err := c.Token(w, r)
		if err != nil {
			http.Error(w, "Erreur interne du serveur", 500)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tokenKey{}, token)))
	})
}
