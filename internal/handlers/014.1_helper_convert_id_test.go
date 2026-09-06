package handlers

import "testing"

func TestParseID(t *testing.T) {
	id, err := parseID("42")

	if err != nil {
		t.Fatalf("erreur inattendue : %v", err)
	}

	if id != 42 {
		t.Errorf("id obtenu : %d, attendu : %d", id, 42)
	}
}

func TestParseIDInvalid(t *testing.T) {
	_, err := parseID("abc")

	if err == nil {
		t.Error("une erreur était attendue")
	}
}
