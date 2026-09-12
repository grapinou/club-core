package views

// SecurityData contains display hints only. Middleware enforces access.
type SecurityData struct {
	Authenticated           bool
	CanWritePersons         bool
	CurrentPath             string
	CanReadPersons          bool
	CanReadMemberships      bool
	CanReviewRegistrations  bool
	RegistrationReviewCount int64
	CSRFToken               string
}
