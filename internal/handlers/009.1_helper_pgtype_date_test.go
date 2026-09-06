package handlers

import (
	"testing"
	"time"
)

func TestHelperPgTypeDate(t *testing.T) {

	dateValue, err := pgTypeDate("1903-02-01")

	if err != nil {
		t.Fatalf("erreur inatendue : %v", err)
	}

	expected := time.Date(1903, time.February, 1, 0, 0, 0, 0, time.UTC)

	if !dateValue.Valid {
		t.Errorf("La date devrait être valide")
	}

	if !dateValue.Time.Equal(expected) {
		t.Errorf("date obtenue : %v, date attendue : %v",
			dateValue.Time,
			expected)
	}

}

func TestHelperPgTypeDateInvalid(t *testing.T) {
	_, err := pgTypeDate("bonjour")

	if err == nil {
		t.Errorf("une erreur était attendue")
	}
}
