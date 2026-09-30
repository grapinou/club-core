package handlers

import (
	"database/sql"
	"testing"
)

func TestHelperPgTypeText(t *testing.T) {

	textValue := pgTypeText("bonjour")

	expected := sql.NullString{
		String: "bonjour",
		Valid:  true,
	}

	if !textValue.Valid {
		t.Errorf("Le texte devrait être valide")
	}

	if textValue.String != expected.String {
		t.Errorf("texte obtenue : %v, texte attendue : %v",
			textValue.String,
			expected.String)
	}

}

func TestHelperPgTypeTextTrimSpace(t *testing.T) {
	textValue := pgTypeText("  bonjour  ")

	if textValue.String != "bonjour" {
		t.Errorf(
			"texte obtenu : %q, texte attendu : %q",
			textValue.String,
			"bonjour",
		)
	}
}

func TestHelperPgTypeTextEmpty(t *testing.T) {
	textValue := pgTypeText("")

	if textValue.Valid {
		t.Errorf("le texte vide devrait être invalide")
	}
}
