// Package civildate holds Club Core's shared civil majority calculation.
package civildate

import "time"

// IsMinor uses the eighteenth birthday, March 1 for February 29 births
// when the anniversary year is not leap. Fifteen is not a permission threshold.
func IsMinor(birth, at time.Time) bool {
	anniversary := time.Date(birth.Year()+18, birth.Month(), birth.Day(), 0, 0, 0, 0, at.Location())
	today := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
	return today.Before(anniversary)
}
