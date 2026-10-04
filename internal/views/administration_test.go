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

func TestTrialMembershipActionInHeader(t *testing.T) {
	for _, tt := range []struct {
		name, status, label, href string
		canManage                 bool
		membership                int32
	}{
		{"attended", "attended", "Préparer une demande d’adhésion", "/persons/8/memberships/new?trial=42", true, 0},
		{"existing membership", "attended", "Voir le dossier d’adhésion", "/memberships/19", true, 19},
		{"membership before attendance", "registered", "Voir le dossier d’adhésion", "/memberships/19", true, 19},
		{"registered", "registered", "", "", true, 0},
		{"cancelled", "cancelled", "", "", true, 0},
		{"no show", "no_show", "", "", true, 0},
		{"reader", "attended", "", "", false, 0},
		{"reader with membership", "attended", "", "", false, 19},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var body strings.Builder
			v := AdministrativeView{Mode: "trial", CanManageMemberships: tt.canManage, Trial: dbsqlc.AdministrativeTrialsRow{ID: 42, PersonID: 8, FirstName: "Léa", LastName: "Dupont", Status: tt.status, MembershipID: tt.membership}}
			if err := RenderAdministrative(&body, v); err != nil {
				t.Fatal(err)
			}
			html := body.String()
			start := strings.Index(html, `<header class="page-header trial-detail-header">`)
			end := strings.Index(html[start:], `</header>`)
			header := html[start : start+end]
			if !strings.Contains(header, "Léa Dupont") || strings.Contains(html, "Après l’essai") {
				t.Fatal("header name or obsolete panel")
			}
			if tt.label != "" {
				if !strings.Contains(header, tt.label) || !strings.Contains(header, `href="`+tt.href+`"`) || strings.Count(html, tt.label) != 1 {
					t.Fatal("header action missing or duplicated")
				}
			} else if strings.Contains(html, "Préparer une demande d’adhésion") || strings.Contains(html, "Voir le dossier d’adhésion") {
				t.Fatal("ineligible action rendered")
			}
		})
	}
}
