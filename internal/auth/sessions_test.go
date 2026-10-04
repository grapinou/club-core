package auth

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/database/dbtypes"
)

type sessionUsers struct{ active bool }

func (u *sessionUsers) GetUserByUsername(context.Context, string) (dbsqlc.User, error) {
	return dbsqlc.User{}, nil
}
func (u *sessionUsers) GetUserByID(_ context.Context, id int32) (dbsqlc.User, error) {
	return dbsqlc.User{ID: id, IsActive: u.active, ActivatedAt: dbtypes.Timestamp{Valid: true}, PasswordHash: sql.NullString{Valid: true, String: "hash"}}, nil
}
func TestSessionLifecycle(t *testing.T) {
	sessions := NewSessions(true)
	now := time.Now()
	sessions.now = func() time.Time { return now }
	r := httptest.NewRequest("GET", "https://club.example.test/", nil)
	w := httptest.NewRecorder()
	if err := sessions.Create(w, r, 42); err != nil {
		t.Fatal(err)
	}
	old := w.Result().Cookies()[0]
	r.AddCookie(old)
	if id, ok := sessions.UserID(r); !ok || id != 42 {
		t.Fatal("lookup")
	}
	w = httptest.NewRecorder()
	if err := sessions.Create(w, r, 42); err != nil {
		t.Fatal(err)
	}
	if _, ok := sessions.UserID(r); ok {
		t.Fatal("old session survived rotation")
	}
	current := w.Result().Cookies()[0]
	r = httptest.NewRequest("GET", "https://club.example.test/", nil)
	r.AddCookie(current)
	users := &sessionUsers{active: true}
	service, err := New(users)
	if err != nil {
		t.Fatal(err)
	}
	observed := false
	middleware := sessions.Middleware(service, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, observed = UserID(r.Context()) }))
	middleware.ServeHTTP(httptest.NewRecorder(), r)
	if !observed {
		t.Fatal("no session context")
	}
	users.active = false
	middleware.ServeHTTP(httptest.NewRecorder(), r)
	if observed {
		t.Fatal("disabled user retains authenticated context")
	}
	if _, ok := sessions.UserID(r); ok {
		t.Fatal("disabled session not revoked")
	}
	users.active = true
	w = httptest.NewRecorder()
	if err = sessions.Create(w, r, 42); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("GET", "https://club.example.test/", nil)
	r.AddCookie(w.Result().Cookies()[0])
	now = now.Add(12 * time.Hour)
	if _, ok := sessions.UserID(r); ok {
		t.Fatal("expired session")
	}
	forged := httptest.NewRequest("GET", "https://club.example.test/", nil)
	forged.AddCookie(&http.Cookie{Name: sessions.CookieName(), Value: "forged"})
	if _, ok := sessions.UserID(forged); ok {
		t.Fatal("forged session")
	}
}

func TestRevokeAccountIncludesUnboundSessions(t *testing.T) {
	s := NewSessions(true)
	var requests []*http.Request
	for _, tc := range []struct {
		id    int32
		bound bool
	}{{42, false}, {42, true}, {43, false}} {
		r := httptest.NewRequest("GET", "https://club.example.test/", nil)
		w := httptest.NewRecorder()
		var err error
		if tc.bound {
			err = s.CreateAuthenticated(w, r, tc.id, [32]byte{1})
		} else {
			err = s.Create(w, r, tc.id)
		}
		if err != nil {
			t.Fatal(err)
		}
		r.AddCookie(w.Result().Cookies()[0])
		requests = append(requests, r)
	}
	s.RevokeAccount(42)
	for i, r := range requests {
		_, ok := s.UserID(r)
		if ok != (i == 2) {
			t.Fatal("account revocation", i, ok)
		}
	}
	s.RevokeAccount(42)
}
