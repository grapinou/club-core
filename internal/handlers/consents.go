package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	"github.com/grapinou/club-core/internal/administration"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/consents"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/personalspace"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
)

// GET and POST both authorize the dossier; the writer rechecks grants within its transaction.
func RegisterConsents(mux *http.ServeMux, site string, q *dbsqlc.Queries, s *consents.Service, personal *personalspace.Service, office *administration.Service, access *Access, csrf *websecurity.CSRF, loc *time.Location) {
	for _, prefix := range []string{"/memberships/{id}", "/me/memberships/{id}", "/me/children/{childID}/memberships/{id}"} {
		isOffice := prefix == "/memberships/{id}"
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, err := personalID(r, "id")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			definition, err := personalID(r, "definition")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			v := views.ConsentPage{SecurityData: pageSecurity(r), SiteName: site, Title: "Modifier une décision - " + site, Action: r.URL.Path, Office: isOffice}
			v.Back = fmt.Sprintf("/me/memberships/%d", id)
			if isOffice {
				m, e := office.Membership(r.Context(), id)
				if e != nil {
					membershipError(w, r, e)
					return
				}
				person, e := office.Person(r.Context(), m.Info.PersonID)
				if e != nil {
					membershipError(w, r, e)
					return
				}
				if !person.IsMinor {
					v.Givers = append(v.Givers, views.ConsentChoice{ID: person.Info.ID, Name: person.Info.FirstName + " " + person.Info.LastName})
				}
				if person.IsMinor {
					for _, g := range person.Relations {
						if g.IsGuardian && !g.Archived {
							v.Givers = append(v.Givers, views.ConsentChoice{ID: g.ID, Name: g.FirstName + " " + g.LastName})
						}
					}
				}
				v.Back = fmt.Sprintf("/memberships/%d", id)
			} else {
				var m personalspace.Membership
				if r.PathValue("childID") != "" {
					child, e := personalID(r, "childID")
					if e != nil {
						http.NotFound(w, r)
						return
					}
					m, err = personal.GetManagedChildMembership(r.Context(), child, id)
					v.Back = fmt.Sprintf("/me/children/%d/memberships/%d", child, id)
				} else {
					m, err = personal.GetMyMembership(r.Context(), id)
				}
				if err != nil {
					personalError(w, r, err)
					return
				}
				if !m.CanDecide {
					http.Error(w, "Accès refusé", 403)
					return
				}
			}
			requirements, err := q.ListMembershipConsentRequirements(r.Context(), id)
			if err != nil {
				membershipError(w, r, err)
				return
			}
			found := false
			for _, c := range requirements {
				if c.ID == definition {
					found = true
					v.ConsentTitle = c.Title
					v.Description = c.Description
					v.Decision = views.DisplayStatus(c.Decision.String).Label
					v.Active = c.IsActive
					v.Granted = c.Decision.String == "granted"
				}
			}
			if !found || (!v.Active && !v.Granted) {
				http.NotFound(w, r)
				return
			}
			if r.Method == "POST" {
				var giver int32
				if isOffice {
					giver, err = formID(r.PostForm, "giver_id", false)
				}
				if err == nil {
					err = s.RecordForActor(r.Context(), dbsqlc.CreateMembershipConsentParams{MembershipID: id, ConsentDefinitionID: definition, GivenByPersonID: giver, Decision: r.PostForm.Get("decision")}, isOffice, access.checker, loc)
				}
				if err == nil {
					http.Redirect(w, r, v.Back, 303)
					return
				}
				v.Error = "La décision n’a pas été enregistrée. Vérifiez la réponse et votre droit d’accès au dossier."
			}
			var buf bytes.Buffer
			if e := views.RenderConsent(&buf, v); e != nil {
				http.Error(w, "Erreur interne du serveur", 500)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if v.Error != "" {
				w.WriteHeader(422)
			}
			_, _ = w.Write(buf.Bytes())
		})
		var protected http.Handler = RequireAuthenticated(csrf.Protect(handler))
		if isOffice {
			protected = access.RequirePermission(authorization.MembershipsApprove, csrf.Protect(handler))
		}
		mux.Handle("GET "+prefix+"/consents/{definition}", protected)
		mux.Handle("POST "+prefix+"/consents/{definition}", protected)
	}
}
