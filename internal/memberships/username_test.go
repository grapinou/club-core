package memberships

import (
	"testing"
	"time"
)

func TestUsernameBase(t *testing.T) {
	for _, tc := range []struct{ first, last, want string }{
		{"Rémi", "Dupont", "remi.dupont"}, {"  Anne-Marie ", "D’Œuf", "anne.marie.d.oeuf"},
		{"Re\u0301mi", "Dupont", "remi.dupont"}, {"", "", "member"}, {"李", "王", "李.王"},
	} {
		if got := UsernameBase(tc.first, tc.last); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.first, got, tc.want)
		}
	}
}
func TestMajorityCivilBirthday(t *testing.T) {
	for _, tc := range []struct {
		birth, date string
		minor       bool
	}{
		{"2008-09-11", "2026-09-10", true}, {"2008-09-11", "2026-09-11", false}, {"2008-09-11", "2026-09-12", false},
		{"2008-02-29", "2026-02-28", true}, {"2008-02-29", "2026-03-01", false},
	} {
		birth, _ := time.Parse("2006-01-02", tc.birth)
		loc, _ := time.LoadLocation("Europe/Paris")
		at, _ := time.ParseInLocation("2006-01-02", tc.date, loc)
		if got := IsMinor(birth, at); got != tc.minor {
			t.Errorf("%+v: %v", tc, got)
		}
	}
}
