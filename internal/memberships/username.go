package memberships

import "github.com/grapinou/club-core/internal/accounts/provisioning"

// UsernameBase preserves the existing API; account naming belongs to provisioning.
func UsernameBase(first, last string) string { return provisioning.UsernameBase(first, last) }
