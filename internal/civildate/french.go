package civildate

import (
	"errors"
	"time"
)

var ErrFrench = errors.New("date must use JJ/MM/AAAA and be a valid civil date")

// ParseFrench accepts exactly ten characters, DD/MM/YYYY, in years 0001–9999.
// Empty/optional fields are handled by callers; no spaces or ISO fallback.
func ParseFrench(value string) (time.Time, error) {
	if len(value) != 10 || value[2] != '/' || value[5] != '/' {
		return time.Time{}, ErrFrench
	}
	for i, c := range value {
		if i != 2 && i != 5 && (c < '0' || c > '9') {
			return time.Time{}, ErrFrench
		}
	}
	t, err := time.Parse("02/01/2006", value)
	if err != nil || t.Year() < 1 {
		return time.Time{}, ErrFrench
	}
	return t, nil
}

// FormatFrench formats civil components without converting the time zone.
func FormatFrench(t time.Time) string { return t.Format("02/01/2006") }
