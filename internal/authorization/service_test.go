package authorization

import (
	"context"
	"errors"
	"testing"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

type reader struct {
	roles []dbsqlc.Role
	err   error
	calls int
}

func (r *reader) ListUserRoles(context.Context, int32) ([]dbsqlc.Role, error) {
	r.calls++
	return r.roles, r.err
}
func TestPermissions(t *testing.T) {
	all := []Permission{PersonsRead, PersonsWrite, MembershipsRead, MembershipsApprove, ActivationResend, RolesRead, RolesManage}
	for _, tc := range []struct {
		name  string
		roles []string
		want  int
	}{
		{"president", []string{"president"}, 7}, {"secretary", []string{"secretary"}, 5},
		{"treasurer", []string{"treasurer"}, 0}, {"coach", []string{"coach"}, 0}, {"no roles", nil, 0},
		{"legacy unknown", []string{"member", "administrator"}, 0},
		{"multiple", []string{"secretary", "treasurer", "coach"}, 5},
		{"union", []string{"secretary", "president"}, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &reader{}
			for _, role := range tc.roles {
				r.roles = append(r.roles, dbsqlc.Role{Name: role})
			}
			svc := New(r)
			for i, p := range all {
				got, err := svc.HasPermission(t.Context(), 1, p)
				if err != nil || got != (i < tc.want) {
					t.Fatalf("%s: %v %v", p, got, err)
				}
			}
			if ok, err := svc.HasPermission(t.Context(), 1, "unknown.permission"); err != nil || ok {
				t.Fatal("unknown capability allowed")
			}
		})
	}
}
func TestNoCacheAndDatabaseFailure(t *testing.T) {
	r := &reader{roles: []dbsqlc.Role{{Name: "secretary"}}}
	s := New(r)
	if ok, _ := s.HasPermission(t.Context(), 1, PersonsRead); !ok {
		t.Fatal("initial permission")
	}
	r.roles = nil
	if ok, _ := s.HasPermission(t.Context(), 1, PersonsRead); ok {
		t.Fatal("cached permission")
	}
	r.err = errors.New("unavailable")
	if ok, err := s.HasPermission(t.Context(), 1, PersonsRead); err == nil || ok {
		t.Fatal("fail open")
	}
	if r.calls != 3 {
		t.Fatal(r.calls)
	}
}
