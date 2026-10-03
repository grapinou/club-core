package views

import "strings"

// SecurityData contains display hints only. Middleware enforces access.
type SecurityData struct {
	AdministrativeRoles     []string
	MetaDescription         string
	Authenticated           bool
	HasPersonalContext      bool
	CanWritePersons         bool
	CurrentPath             string
	CanReadPersons          bool
	CanReadUsers            bool
	CanManageRoles          bool
	CanConfigureClub        bool
	CanReadMemberships      bool
	CanReviewRegistrations  bool
	RegistrationReviewCount int64
	TodayTrialCount         int64
	CSRFToken               string
}

// Navigation hints never replace route authorization.
func (s SecurityData) InSection(path string) bool {
	return s.CurrentPath == path || strings.HasPrefix(s.CurrentPath, path+"/")
}

func (s SecurityData) PrivatePage() bool {
	if !s.Authenticated {
		return false
	}
	for _, path := range []string{"/members", "/guardians", "/prospects", "/dashboard", "/me", "/admin", "/persons", "/trials", "/memberships", "/registration-reviews"} {
		if s.InSection(path) {
			return true
		}
	}
	return false
}
