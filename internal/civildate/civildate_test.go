package civildate

import (
	"testing"
	"time"
)

func TestMinorCivilAnniversary(t *testing.T) {
	birth := time.Date(2008, 2, 29, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		at    string
		minor bool
	}{{"2026-02-28", true}, {"2026-03-01", false}, {"2023-03-01", true}} {
		at, err := time.Parse("2006-01-02", tc.at)
		if err != nil {
			t.Fatal(err)
		}
		if IsMinor(birth, at) != tc.minor {
			t.Fatal(tc)
		}
	}
}
