package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/database/dbtypes"
)

type trialNavigationPermissions struct {
	allowed bool
	err     error
}

func (p trialNavigationPermissions) HasPermission(_ context.Context, _ int32, permission authorization.Permission) (bool, error) {
	if permission == authorization.PersonsRead {
		return p.allowed, p.err
	}
	return false, nil
}

type trialNavigationCounter struct{ calls int }

func (c *trialNavigationCounter) Counts(context.Context) (dbsqlc.AdministrativeCountsRow, error) {
	c.calls++
	return dbsqlc.AdministrativeCountsRow{TodayTrials: 7}, nil
}

type trialNavigationUser struct{}

func (trialNavigationUser) GetUserByUsername(context.Context, string) (dbsqlc.User, error) {
	return dbsqlc.User{}, nil
}
func (trialNavigationUser) GetUserByID(_ context.Context, id int32) (dbsqlc.User, error) {
	return dbsqlc.User{ID: id, IsActive: true, ActivatedAt: dbtypes.Timestamp{Valid: true}, PasswordHash: sql.NullString{Valid: true, String: "credential"}}, nil
}
func TestTrialNavigationOnlyCountsAuthorizedReaders(t *testing.T) {
	service, err := auth.New(trialNavigationUser{})
	if err != nil {
		t.Fatal(err)
	}
	sessions := auth.NewSessions(true)
	for _, tt := range []struct {
		name                   string
		authenticated, allowed bool
		err                    error
		wantCalls              int
	}{
		{"reader", true, true, nil, 1},
		{"no read permission", true, false, nil, 0},
		{"permission error", true, true, errors.New("failure"), 0},
		{"anonymous", false, true, nil, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			counter := &trialNavigationCounter{}
			access := NewAccess("Club", trialNavigationPermissions{tt.allowed, tt.err})
			access.SetTrialCounter(counter)
			r := httptest.NewRequest("GET", "https://club.example.test/trials", nil)
			if tt.authenticated {
				w := httptest.NewRecorder()
				if err := sessions.Create(w, r, 42); err != nil {
					t.Fatal(err)
				}
				r.AddCookie(w.Result().Cookies()[0])
			}
			handler := sessions.Middleware(service, access.Navigation(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				data := pageSecurity(r)
				if tt.wantCalls == 1 {
					if !data.CanReadPersons || data.TodayTrialCount != 7 {
						t.Fatal("authorized count absent")
					}
				} else if data.TodayTrialCount != 0 || data.CanReadPersons {
					t.Fatal("count or permission leaked")
				}
			})))
			handler.ServeHTTP(httptest.NewRecorder(), r)
			if counter.calls != tt.wantCalls {
				t.Fatalf("count calls %d want %d", counter.calls, tt.wantCalls)
			}
		})
	}
}
