package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/passwordreset"
	"github.com/grapinou/club-core/internal/websecurity"
)

type recoveryStub struct {
	submitted []string
	err       error
}

func (s *recoveryStub) Submit(username string)                              { s.submitted = append(s.submitted, username) }
func (s *recoveryStub) Validate(context.Context, string) error              { return s.err }
func (s *recoveryStub) Reset(context.Context, string, string, string) error { return s.err }
func (s *recoveryStub) RequestForPerson(context.Context, int32) error       { return nil }

func TestPublicRecoveryNeutralAndSeparateBudgets(t *testing.T) {
	s := &recoveryStub{}
	h := NewPasswordResetHandler("Club Core", s)
	csrf := websecurity.NewCSRF(false)
	mux := http.NewServeMux()
	h.Register(mux, nil, csrf)
	// Register only public routes without dereferencing access on request.
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest("GET", "http://localhost/password/forgot", nil))
	cookie := get.Result().Cookies()[0]
	var body string
	for i, username := range []string{"eligible", "unknown", "disabled", "unactivated", "no-email", "over-budget"} {
		form := url.Values{"username": {username}, "csrf_token": {cookie.Value}}
		r := httptest.NewRequest("POST", "http://localhost/password/forgot", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(cookie)
		r.RemoteAddr = "192.0.2.1:12345"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 303 || w.Header().Get("Location") != "/password/forgot?sent=1" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("public response", w.Code, w.Header())
		}
		if i == 0 {
			body = w.Body.String()
		} else if w.Body.String() != body {
			t.Fatal("enumeration")
		}
	}
	if len(s.submitted) != 5 {
		t.Fatal("IP budget", s.submitted)
	}
	if !NewAttemptLimiter().Allow("192.0.2.1:12345") {
		t.Fatal("login affected")
	}
	get = httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest("GET", "http://localhost/password/forgot?sent=1", nil))
	if !strings.Contains(get.Body.String(), recoveryNotice) || strings.Contains(get.Body.String(), "name=\"token\"") {
		t.Fatal("neutral confirmation")
	}
	for _, path := range []string{"/password/forgot", "/password/reset"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "http://localhost"+path, strings.NewReader("username=eligible")))
		if w.Code != 403 {
			t.Fatal("CSRF", path, w.Code)
		}
	}
}

func TestRecoveryLimiterBoundsAndIgnoresForwardedHeaders(t *testing.T) {
	l := NewPasswordRecoveryLimiter()
	now := time.Now()
	l.now = func() time.Time { return now }
	r := httptest.NewRequest("POST", "http://localhost/password/forgot", nil)
	r.RemoteAddr = "192.0.2.1:1"
	for i := 0; i < 5; i++ {
		r.Header.Set("X-Forwarded-For", string(rune('a'+i)))
		if !l.AllowRequest(r) {
			t.Fatal("early limit")
		}
	}
	r.RemoteAddr = "192.0.2.1:2"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	if l.AllowRequest(r) {
		t.Fatal("forwarded header trusted")
	}
	now = now.Add(15 * time.Minute)
	if !l.AllowRequest(r) {
		t.Fatal("window did not expire")
	}
	l.globalLimit = 20000
	for i := 0; i < 10001; i++ {
		l.Allow(string(rune(i + 100)))
	}
	if len(l.ips) > 10000 {
		t.Fatal("unbounded recovery limiter")
	}
}

func TestInvalidResetDoesNotReflectToken(t *testing.T) {
	for _, err := range []error{passwordreset.ErrInvalidToken, errors.New("private internal detail")} {
		h := NewPasswordResetHandler("Club", &recoveryStub{err: err})
		protected := websecurity.NewCSRF(false).Protect(http.HandlerFunc(h.reset))
		w := httptest.NewRecorder()
		protected.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/password/reset?token=private-secret", nil))
		if strings.Contains(w.Body.String(), "private-secret") || strings.Contains(w.Body.String(), "private internal") {
			t.Fatal("token/error leak")
		}
	}
}
