package trials

import (
	"testing"
	"time"
)

func TestPublicWindowCivilToday(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ at, first, last string }{
		{"2026-09-28T10:00:00Z", "2026-09-28", "2026-10-19"},
		{"2026-10-02T10:00:00Z", "2026-10-02", "2026-10-23"},
		{"2026-10-04T19:00:00Z", "2026-10-04", "2026-10-25"},
		{"2026-10-04T22:30:00Z", "2026-10-05", "2026-10-26"},
		{"2026-10-25T22:59:59Z", "2026-10-25", "2026-11-15"},
		{"2026-10-25T23:00:00Z", "2026-10-26", "2026-11-16"},
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

func TestPublicSessionCivilStart(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		at, date, start string
		want            bool
	}{
		{"2026-10-02T12:00:00Z", "2026-10-02", "19:00:00", true},
		{"2026-10-02T12:00:00Z", "2026-10-02", "10:00:00", false},
		{"2026-10-02T12:00:00Z", "2026-10-02", "14:00:00", false},
		{"2026-10-02T12:00:20Z", "2026-10-02", "14:00:30", true},
		{"2026-10-02T12:00:30Z", "2026-10-02", "14:00:30", false},
		{"2026-10-04T22:30:00Z", "2026-10-05", "00:45:00", true},
		{"2026-10-04T22:30:00Z", "2026-10-05", "00:15:00", false},
		{"2026-10-25T23:30:00Z", "2026-10-26", "00:45:00", true},
		{"2026-10-25T23:30:00Z", "2026-10-26", "00:15:00", false},
		{"2026-10-02T12:00:00Z", "2026-10-02", "invalid", false},
	} {
		t.Run(tc.at+tc.start, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, tc.at)
			date, _ := time.Parse("2006-01-02", tc.date)
			if got := sessionAhead(date, tc.start, now, loc); got != tc.want {
				t.Fatal("session start", got, tc.want)
			}
		})
	}
}
