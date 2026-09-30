package trials

import (
	"testing"
	"time"
)

func TestPublicWindowCivilWeek(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ at, first, last string }{
		{"2026-09-28T10:00:00Z", "2026-10-05", "2026-10-19"},
		{"2026-10-02T10:00:00Z", "2026-10-05", "2026-10-23"},
		{"2026-10-04T19:00:00Z", "2026-10-05", "2026-10-25"},
		{"2026-10-04T22:30:00Z", "2026-10-12", "2026-10-26"},
		{"2026-10-25T22:59:59Z", "2026-10-26", "2026-11-15"},
		{"2026-10-25T23:00:00Z", "2026-11-02", "2026-11-16"},
	} {
		t.Run(tc.at, func(t *testing.T) {
			at, _ := time.Parse(time.RFC3339, tc.at)
			first, last := PublicWindow(at, loc)
			if first.Format("2006-01-02") != tc.first || last.Format("2006-01-02") != tc.last {
				t.Fatalf("%s – %s", first, last)
			}
		})
	}
}
