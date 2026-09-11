package handlers

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

type rejectingUsers struct{}

func (rejectingUsers) GetUserByUsername(context.Context, string) (dbsqlc.User, error) {
	return dbsqlc.User{}, errors.New("missing")
}
func (rejectingUsers) GetUserByID(context.Context, int32) (dbsqlc.User, error) {
	return dbsqlc.User{}, errors.New("missing")
}

type recordingActivator struct{ calls int }

func (a *recordingActivator) Activate(context.Context, string, string, string) (dbsqlc.User, error) {
	a.calls++
	return dbsqlc.User{}, errors.New("private service diagnostic")
}
func TestAuthCSRFAndSecretRedaction(t *testing.T) {
	login, err := auth.New(rejectingUsers{})
	if err != nil {
		t.Fatal(err)
	}
	activation := &recordingActivator{}
	h := NewAuthHandler("Club Core", activation, login, auth.NewSessions(true), true)
	mux := http.NewServeMux()
	h.Register(mux)
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest("GET", "https://club.example.test/activate", nil))
	if get.Code != 200 || get.Header().Get("Cache-Control") != "no-store" || get.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("GET protections")
	}
	var csrf *http.Cookie
	for _, c := range get.Result().Cookies() {
		if c.Name == "__Host-club_csrf" {
			csrf = c
		}
	}
	if csrf == nil || !csrf.Secure || !csrf.HttpOnly || csrf.SameSite != http.SameSiteStrictMode {
		t.Fatal("CSRF cookie")
	}
	var logs bytes.Buffer
	original := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(original)
	for _, tc := range []struct {
		name, token, origin, fetch string
		cookie                     bool
		want                       int
	}{
		{"no cookie", csrf.Value, "https://club.example.test", "", false, 403},
		{"no token", "", "https://club.example.test", "", true, 403},
		{"bad token", strings.Repeat("x", 64), "https://club.example.test", "", true, 403},
		{"foreign origin", csrf.Value, "https://attacker.example.test", "", true, 403},
		{"foreign fetch", csrf.Value, "", "cross-site", true, 403},
		{"valid", csrf.Value, "https://club.example.test", "same-origin", true, 303},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{"csrf_token": {tc.token}, "username": {"private.username"}, "code": {"private-code"}, "password": {"private-password"}, "confirmation": {"private-password"}}
			r := httptest.NewRequest("POST", "https://club.example.test/activate", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.fetch)
			if tc.cookie {
				r.AddCookie(csrf)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatal("status", w.Code, tc.want)
			}
			output := w.Body.String() + w.Header().Get("Location") + logs.String()
			for _, secret := range []string{"private-code", "private-password", "private.username", "private service diagnostic"} {
				if strings.Contains(output, secret) {
					t.Fatal("secret leaked")
				}
			}
		})
	}
	if activation.calls != 1 {
		t.Fatal("CSRF reached business service", activation.calls)
	}
	// An oversized POST is rejected without calling the service.
	form := url.Values{"csrf_token": {csrf.Value}, "password": {strings.Repeat("x", 9000)}}
	r := httptest.NewRequest("POST", "https://club.example.test/activate", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(csrf)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 400 || activation.calls != 1 {
		t.Fatal("body limit")
	}
}
func TestAttemptLimiter(t *testing.T) {
	now := time.Now()
	limiter := NewAttemptLimiter()
	limiter.now = func() time.Time { return now }
	for i := 0; i < 10; i++ {
		if !limiter.Allow("192.0.2.1:1234") {
			t.Fatal("premature limit")
		}
	}
	if limiter.Allow("192.0.2.1:5678") {
		t.Fatal("port bypass")
	}
	if !limiter.Allow("192.0.2.2:1234") {
		t.Fatal("IP isolation")
	}
	now = now.Add(15 * time.Minute)
	if !limiter.Allow("192.0.2.1:1234") {
		t.Fatal("window did not expire")
	}
	limiter = NewAttemptLimiter()
	limiter.now = func() time.Time { return now }
	for i := 0; i < 120; i++ {
		// Different textual IPs are only test keys; network requests use RemoteAddr.
		if !limiter.Allow(time.Unix(int64(i), 0).String()) {
			t.Fatal("premature global limit")
		}
	}
	if limiter.Allow("new IP") {
		t.Fatal("missing global budget")
	}
	now = now.Add(time.Minute)
	if !limiter.Allow("new IP") {
		t.Fatal("global expiry")
	}
}
func TestHTTPAttemptLimitIgnoresForwardedIP(t *testing.T) {
	login, err := auth.New(rejectingUsers{})
	if err != nil {
		t.Fatal(err)
	}
	a := &recordingActivator{}
	h := NewAuthHandler("Club Core", a, login, auth.NewSessions(false), false)
	mux := http.NewServeMux()
	h.Register(mux)
	for i := 0; i < 11; i++ {
		r := httptest.NewRequest("POST", "/activate", nil)
		r.Header.Set("X-Forwarded-For", time.Unix(int64(i), 0).String())
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if i == 10 && (w.Code != 429 || w.Header().Get("Retry-After") == "") {
			t.Fatal("missing HTTP limit")
		}
	}
	if a.calls != 0 {
		t.Fatal("invalid calls reached service")
	}
}
