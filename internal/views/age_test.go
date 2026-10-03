package views

import (
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/database/dbtypes"
)

func TestAgeAt(t *testing.T) {
	date := func(s string) dbtypes.Date {
		if s == "" {
			return dbtypes.Date{}
		}
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return dbtypes.Date{Time: d, Valid: true}
	}
	for _, tt := range []struct {
		name, birth, trial string
		want               int
	}{
		{"birthday passed", "2014-10-10", "2026-10-11", 12},
		{"birthday today", "2014-10-10", "2026-10-10", 12},
		{"birthday later", "2014-10-10", "2026-10-03", 11},
		{"year boundary", "2014-12-31", "2026-01-01", 11},
		{"missing birth", "", "2026-10-03", -1},
		{"missing trial", "2014-10-10", "", -1},
		{"birth after trial", "2026-10-10", "2026-10-03", -1},
		{"leap birthday before", "2012-02-29", "2026-02-28", 13},
		{"leap birthday after", "2012-02-29", "2026-03-01", 14},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := AgeAt(date(tt.birth), date(tt.trial))
			if tt.want < 0 {
				if got != nil {
					t.Fatalf("want nil got %d", *got)
				}
				return
			}
			if got == nil || *got != tt.want {
				t.Fatalf("age=%v want %d", got, tt.want)
			}
		})
	}
	// Civil date is preserved even when the instant would be a different UTC day.
	got := AgeAt(date("2014-10-10"), dbtypes.Date{Valid: true, Time: time.Date(2026, 10, 10, 0, 0, 0, 0, time.FixedZone("Paris", 7200))})
	if got == nil || *got != 12 {
		t.Fatal("age must use civil trial date")
	}
}
