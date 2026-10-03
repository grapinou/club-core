package views

import "github.com/grapinou/club-core/internal/database/dbtypes"

// AgeAt returns the age on the trial's civil date, independently of time zones.
// A missing birth/trial date (or a birth after the trial) has no displayable age.
func AgeAt(birth, on dbtypes.Date) *int {
	if !birth.Valid || !on.Valid {
		return nil
	}
	by, bm, bd := birth.Time.Date()
	y, m, d := on.Time.Date()
	age := y - by
	if m < bm || (m == bm && d < bd) {
		age--
	}
	if age < 0 {
		return nil
	}
	return &age
}
