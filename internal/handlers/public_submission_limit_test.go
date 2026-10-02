package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/grapinou/club-core/internal/organization"
	"github.com/grapinou/club-core/internal/registrationapplications"
	"github.com/grapinou/club-core/internal/trials"
	"github.com/grapinou/club-core/internal/websecurity"
)

func publicLimitHandler(t *testing.T) (http.Handler, *AttemptLimiter, *time.Time) {
	t.Helper()
	db, err := database.New(t.Context(), filepath.Join(t.TempDir(), "public-limit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = database.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, loc)
	limiter := NewRegistrationSubmissionLimiter()
	limiter.now = func() time.Time { return now }
	csrf := websecurity.NewCSRF(false)
	mux := http.NewServeMux()
	public := csrf.Protect(NewPublicHandler(organization.New(db), loc, "", trials.NewPublic(db, loc), limiter, nil, "", WithPublicClock(func() time.Time { return now })))
	mux.Handle("GET /essai", public)
	mux.Handle("POST /essai", public)
	m, err := memberships.New(db, time.Hour, loc)
	if err != nil {
		t.Fatal(err)
	}
	applications, err := registrationapplications.New(db, identityresolution.NewSubmitter(db), m)
	if err != nil {
		t.Fatal(err)
	}
	NewJoinHandler("Club Core", applications, limiter).Register(mux, csrf)
	return mux, limiter, &now
}

func TestPublicSubmissionActionsUseOnlyDurableBudget(t *testing.T) {
	for _, tc := range []struct{ path, key, prepare, final string }{
		{"/essai", "step", "contacts", "book"},
		{"/join", "action", "review", "submit"},
		{"/join/child", "action", "review", "submit"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			handler, limiter, now := publicLimitHandler(t)
			get := httptest.NewRecorder()
			handler.ServeHTTP(get, httptest.NewRequest("GET", tc.path, nil))
			if get.Code != 200 {
				t.Fatal("GET", get.Code)
			}
			cookie := get.Result().Cookies()[0]
			call := func(method, path string, values url.Values) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
				r.RemoteAddr = "192.0.2.1:1234"
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.AddCookie(cookie)
				// Forwarded addresses cannot create additional budgets.
				r.Header.Set("X-Forwarded-For", fmt.Sprint(limiter.global.count))
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				return w
			}
			prepare := url.Values{"csrf_token": {cookie.Value}, tc.key: {tc.prepare}}
			for i := 0; i < 12; i++ {
				if r := call("GET", tc.path+"?"+tc.key+"="+tc.final, nil); r.Code != 200 {
					t.Fatal("GET consumed budget", r.Code)
				}
				// Empty catalog and fields deliberately exercise non-durable validation errors.
				if r := call("POST", tc.path, prepare); r.Code != 422 {
					t.Fatal("preparation", r.Code)
				}
				if tc.key == "action" {
					edit := url.Values{"csrf_token": {cookie.Value}, "action": {"edit"}}
					if r := call("POST", tc.path, edit); r.Code != 422 {
						t.Fatal("edit", r.Code)
					}
				}
			}
			if limiter.global.count != 0 || len(limiter.ips) != 0 {
				t.Fatal("preparations spent budget")
			}
			final := url.Values{"csrf_token": {cookie.Value}, tc.key: {tc.final}}
			for _, token := range []string{"", "invalid"} {
				badCSRF := url.Values{"csrf_token": {token}, tc.key: {tc.final}}
				if r := call("POST", tc.path, badCSRF); r.Code != 403 || limiter.global.count != 0 {
					t.Fatal("CSRF", r.Code)
				}
			}
			for i := 0; i < 5; i++ {
				if r := call("POST", tc.path, final); r.Code != 422 {
					t.Fatal("final validation must spend budget", i, r.Code)
				}
			}
			if limiter.global.count != 5 {
				t.Fatal("submission count", limiter.global.count)
			}
			if r := call("POST", tc.path, final); r.Code != 429 || r.Header().Get("Retry-After") != "900" || r.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("sixth submission", r.Code, r.Header())
			}
			if r := call("POST", tc.path, prepare); r.Code != 422 {
				t.Fatal("preparation blocked after exhaustion", r.Code)
			}
			// The budget is shared by trial and membership handlers, not reset by a route.
			other, key, value := "/essai", "step", "book"
			if tc.path == "/essai" {
				other, key, value = "/join", "action", "submit"
			}
			if r := call("POST", other, url.Values{"csrf_token": {cookie.Value}, key: {value}}); r.Code != 429 {
				t.Fatal("split public budgets", r.Code)
			}
			*now = now.Add(15 * time.Minute)
			if r := call("POST", tc.path, final); r.Code != 422 {
				t.Fatal("budget did not reset", r.Code)
			}
		})
	}
}
