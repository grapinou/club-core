package views

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grapinou/club-core/internal/personalspace"
)

func TestDashboardRetainsMembershipLinkOutsideOpenSeasons(t *testing.T) {
	const membershipURL = "/me/children/42/memberships/7"
	for _, action := range []string{"", "Voir le dossier"} {
		child := personalspace.ChildSummary{ID: 42, Name: "Arthur Dupont", Memberships: []personalspace.Summary{{ID: 7, SeasonName: "2026/2027", Status: "pending"}}}
		if action != "" {
			child.Actions = []personalspace.FamilyAction{{Season: "2026/2027", Label: action, URL: membershipURL}}
		}
		v := DashboardView{SiteName: "Club Core", Dashboard: personalspace.Dashboard{FirstName: "Claire", Children: []personalspace.ChildSummary{child}}}
		var b bytes.Buffer
		if err := RenderDashboard(&b, v); err != nil {
			t.Fatal(err)
		}
		if strings.Count(b.String(), `href="`+membershipURL+`"`) != 1 {
			t.Fatal("missing or duplicate child membership action")
		}
		if action != "" && !strings.Contains(b.String(), ">Voir le dossier · 2026/2027") {
			t.Fatal("FamilyAction label replaced")
		}
	}
}

func TestChildMembershipReturnUsesAuthorizedFirstName(t *testing.T) {
	for _, tc := range []struct{ name, label string }{{"Arthur", "Retour au dossier d’Arthur"}, {"Éloïse", "Retour au dossier d’Éloïse"}, {"Louise", "Retour au dossier de Louise"}, {"Jean Pierre", "Retour au dossier de Jean Pierre"}} {
		var b bytes.Buffer
		v := PersonalPage{SiteName: "Club Core", Membership: &PersonalMembershipView{ChildID: 42, Membership: personalspace.Membership{ChildFirstName: tc.name}}}
		if err := RenderPersonal(&b, v); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), `href="/me/children/42">`+tc.label) || strings.Contains(b.String(), ">Retour à mon espace</a>") {
			t.Fatal("incorrect child context", tc.name)
		}
	}
}
