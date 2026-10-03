package views

import (
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"strings"
	"testing"
)

func TestTrialResultReadableWithoutWritePermission(t *testing.T) {
	var body strings.Builder
	v := AdministrativeView{Mode: "trial", SecurityData: SecurityData{Authenticated: true, CanReadPersons: true}, Trial: dbsqlc.AdministrativeTrialsRow{FirstName: "Lecture", Status: "attended"}}
	if err := RenderAdministrative(&body, v); err != nil {
		t.Fatal(err)
	}
	html := body.String()
	if !strings.Contains(html, "Résultat de l’essai") || !strings.Contains(html, "Présent") {
		t.Fatal("readable result missing")
	}
	for _, action := range []string{"/status", "/notes", "/reschedule", "Préparer une demande d’adhésion"} {
		if strings.Contains(html, action) {
			t.Fatal("unauthorized action", action)
		}
	}
}
