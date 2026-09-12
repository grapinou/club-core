package views

import "testing"

func TestStatusVocabulary(t *testing.T) {
	for code, want := range map[string]string{"pending": "En attente", "active": "Active", "resolved": "Résolue", "needs_review": "À vérifier", "awaiting_identity": "Identité à vérifier", "awaiting_email_verification": "Vérification email en cours", "refused": "Refusé", "granted": "Accordé"} {
		if DisplayStatus(code).Label != want {
			t.Fatal(code)
		}
	}
	if DisplayStatus("refused").Label == DisplayStatus("").Label {
		t.Fatal("refusal confused with unanswered consent")
	}
}
