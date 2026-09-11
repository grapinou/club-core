package websecurity

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSharedCSRFToken(t *testing.T) {
	csrf := NewCSRF(true)
	var token string
	called := 0
	handler := csrf.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; token = Token(r.Context()) }))
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest("GET", "https://club.example.test/persons/new", nil))
	if len(token) != 64 || len(get.Result().Cookies()) != 1 {
		t.Fatal("GET token generation")
	}
	cookie := get.Result().Cookies()[0]
	for _, tc := range []struct {
		name          string
		cookie        bool
		value, origin string
		want          int
	}{
		{"missing token", true, "", "https://club.example.test", 403},
		{"missing cookie", false, token, "https://club.example.test", 403},
		{"cross origin", true, token, "https://foreign.example.test", 403},
		{"valid", true, token, "https://club.example.test", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{"csrf_token": {tc.value}}
			r := httptest.NewRequest("POST", "https://club.example.test/persons", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", tc.origin)
			if tc.cookie {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatal(w.Code)
			}
		})
	}
	if called != 2 {
		t.Fatal("invalid POST reached handler")
	}
}
