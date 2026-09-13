package auth

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type credentialUsers struct {
	sessionUsers
	hash string
}

func (u *credentialUsers) GetUserByID(_ context.Context, id int32) (dbsqlc.User, error) {
	return dbsqlc.User{ID: id, IsActive: true, ActivatedAt: pgtype.Timestamptz{Valid: true}, PasswordHash: pgtype.Text{String: u.hash, Valid: true}}, nil
}
func TestObsoleteLoginCredentialCannotCreateUsableSession(t *testing.T) {
	users := &credentialUsers{hash: "new hash"}
	service, err := New(users)
	if err != nil {
		t.Fatal(err)
	}
	sessions := NewSessions(false)
	// A login checked the old credential before a concurrent password change,
	// then inserts its session after the change/rotation has committed.
	old := sha256.Sum256([]byte("old hash"))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "http://club.test/", nil)
	if err = sessions.CreateAuthenticated(w, r, 1, old); err != nil {
		t.Fatal(err)
	}
	r.AddCookie(w.Result().Cookies()[0])
	seen := false
	sessions.Middleware(service, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { _, seen = UserID(r.Context()) })).ServeHTTP(httptest.NewRecorder(), r)
	if seen {
		t.Fatal("obsolete login survived credential change")
	}
	if _, ok := sessions.UserID(r); ok {
		t.Fatal("obsolete session not deleted")
	}
}
