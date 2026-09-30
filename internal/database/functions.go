package database

import (
	"database/sql/driver"
	"regexp"
	"strings"

	"modernc.org/sqlite"
)

// Validation is shared across all connections, including migrations. These
// deterministic Go functions preserve the original schema's text constraints.
func init() {
	patterns := map[string]string{"valid_public_email": `^[^\s@]+@[^\s@]+[.][^\s@]+$`, "valid_public_url": `^https?://[^\s/?#@]+([/?#][^\s]*)?$`, "valid_link_kind": `^[a-z][a-z0-9_-]*$`}
	for name, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		sqlite.MustRegisterDeterministicScalarFunction(name, 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			s, _ := args[0].(string)
			return re.MatchString(s), nil
		})
	}
	sqlite.MustRegisterDeterministicScalarFunction("normalize_phone", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		s, _ := args[0].(string)
		return strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, s), nil
	})
	sqlite.MustRegisterDeterministicScalarFunction("unicode_lower", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		s, _ := args[0].(string)
		return strings.ToLower(s), nil
	})
}
