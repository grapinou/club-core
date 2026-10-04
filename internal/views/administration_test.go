package views

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
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

func TestTrialListHeaderActionsRespectWritePermission(t *testing.T) {
	for _, canWrite := range []bool{false, true} {
		var body strings.Builder
		v := AdministrativeView{Mode: "trials", SecurityData: SecurityData{CanReadPersons: true, CanWritePersons: canWrite}}
		if err := RenderAdministrative(&body, v); err != nil {
			t.Fatal(err)
		}
		html := body.String()
		start := strings.Index(html, `<div class="page-header-actions">`)
		if start < 0 {
			t.Fatal("header actions missing")
		}
		end := strings.Index(html[start:], `</div>`)
		actions := html[start : start+end]
		if strings.Contains(actions, `href="/trials/new"`) != canWrite || !strings.Contains(actions, `class="btn btn-primary" href="/trials?all=1"`) {
			t.Fatal("header action permission or management button")
		}
	}
}

func TestCompactTrialResultsPreserveMembershipActions(t *testing.T) {
	for _, tt := range []struct {
		name, status, action string
		canRead, canManage   bool
		membership           int32
	}{
		{"prepare", "attended", `/persons/8/memberships/new?trial=42`, true, true, 0},
		{"open", "attended", `/memberships/19`, true, true, 19},
		{"read existing", "registered", `/memberships/19`, true, false, 19},
		{"no membership access", "attended", "", false, false, 19},
		{"no prepare permission", "attended", "", true, false, 0},
		{"registered", "registered", "", true, true, 0},
		{"cancelled", "cancelled", "", true, true, 0},
		{"no show", "no_show", "", true, true, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var body strings.Builder
			v := AdministrativeView{Mode: "trials", TrialManagement: true, CanManageMemberships: tt.canManage,
				SecurityData: SecurityData{CanReadPersons: true, CanReadMemberships: tt.canRead},
				Trials:       []dbsqlc.AdministrativeTrialsRow{{ID: 42, PersonID: 8, FirstName: "Léa", LastName: "Dupont", ActivityName: "JJB", GroupName: "Adultes", Status: tt.status, MembershipID: tt.membership, Notes: sql.NullString{Valid: true, String: "Matériel"}}}}
			if err := RenderAdministrative(&body, v); err != nil {
				t.Fatal(err)
			}
			html := body.String()
			start := strings.Index(html, `<article class="trial-management-row">`)
			if start < 0 {
				t.Fatal("compact result missing")
			}
			end := strings.Index(html[start:], `</article>`)
			row := html[start : start+end]
			for _, text := range []string{`href="/trials/42">Léa Dupont</a>`, "JJB", "Adultes", v.StatusLabel(tt.status), "Note ou matériel à consulter"} {
				if !strings.Contains(row, text) {
					t.Fatal("result information missing", text)
				}
			}
			if strings.Index(row, "Léa Dupont") > strings.Index(row, "JJB") || strings.Index(row, "JJB") > strings.Index(row, v.StatusLabel(tt.status)) {
				t.Fatal("person, activity and status hierarchy")
			}
			if tt.action != "" {
				if !strings.Contains(row, `class="small" href="`+tt.action+`"`) {
					t.Fatal("secondary membership action missing")
				}
			} else if strings.Contains(row, "Ouvrir l’adhésion") || strings.Contains(row, "Préparer l’adhésion") {
				t.Fatal("ineligible membership action rendered")
			}
			if strings.Contains(row, `class="btn`) || strings.Count(row, `href="/trials/42"`) != 1 {
				t.Fatal("result should use one main link and no competing buttons")
			}
		})
	}
}
