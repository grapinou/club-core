package handlers

import "testing"

func TestFrenchBirthDateOptional(t *testing.T) {
	if date, err := frenchBirthDate("", true); err != nil || date.Valid {
		t.Fatal("optional empty", date, err)
	}
	if _, err := frenchBirthDate("", false); err == nil {
		t.Fatal("required empty accepted")
	}
	if date, err := frenchBirthDate("17/04/2012", false); err != nil || !date.Valid || date.Time.Format("2006-01-02") != "2012-04-17" {
		t.Fatal("civil date conversion", date, err)
	}
}
