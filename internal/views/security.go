package views

// SecurityData contains display hints only. Middleware enforces access.
type SecurityData struct {
	CanReadPersons          bool
	CanReadMemberships      bool
	CanReviewRegistrations  bool
	RegistrationReviewCount int64
	CSRFToken               string
}
