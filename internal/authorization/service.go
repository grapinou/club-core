// Package authorization maps persisted administrative roles to capabilities.
// Memberships never participate in this policy. No role cache is maintained.
package authorization

import (
	"context"
	"errors"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

type Permission string

const (
	PersonsRead         Permission = "persons.read"
	PersonsWrite        Permission = "persons.write"
	MembershipsRead     Permission = "memberships.read"
	MembershipsApprove  Permission = "memberships.approve"
	ActivationResend    Permission = "activation.resend"
	RegistrationsReview Permission = "registrations.review"
	RolesRead           Permission = "roles.read"
	RolesManage         Permission = "roles.manage"
	ClubConfigure       Permission = "club.configure"
)

var ErrForbidden = errors.New("permission denied")
var policy = map[string][]Permission{
	"president": {PersonsRead, PersonsWrite, MembershipsRead, MembershipsApprove, ActivationResend, RegistrationsReview, RolesRead, RolesManage, ClubConfigure},
	"secretary": {PersonsRead, PersonsWrite, MembershipsRead, MembershipsApprove, ActivationResend, RegistrationsReview},
	"treasurer": {},
	"coach":     {},
}

func KnownRole(name string) bool { _, ok := policy[name]; return ok }

// RolesWith keeps the administrative lockout check tied to the same policy as
// request authorization, including if the policy changes later.
func RolesWith(permission Permission) []string {
	var names []string
	for name, permissions := range policy {
		for _, p := range permissions {
			if p == permission {
				names = append(names, name)
				break
			}
		}
	}
	return names
}

type Permissions map[Permission]bool

func (p Permissions) Has(permission Permission) bool { return p[permission] }

type RoleReader interface {
	ListUserRoles(context.Context, int32) ([]dbsqlc.Role, error)
}
type Service struct{ roles RoleReader }

func New(roles RoleReader) *Service { return &Service{roles: roles} }
func (s *Service) Permissions(ctx context.Context, userID int32) (Permissions, error) {
	roles, err := s.roles.ListUserRoles(ctx, userID)
	if err != nil {
		return nil, err
	}
	permissions := Permissions{}
	for _, role := range roles {
		for _, p := range policy[role.Name] {
			permissions[p] = true
		}
	}
	return permissions, nil
}
func (s *Service) HasPermission(ctx context.Context, userID int32, p Permission) (bool, error) {
	permissions, err := s.Permissions(ctx, userID)
	if err != nil {
		return false, err
	}
	return permissions.Has(p), nil
}
