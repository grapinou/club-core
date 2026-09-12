package handlers

import (
	"bytes"
	"net/http"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/guardianaccess"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
)

// DashboardHandler composes read-only summaries. A browser-supplied Person ID
// never selects the data: own identity comes from session, children from grants.
func DashboardHandler(site string, q *dbsqlc.Queries, g *guardianaccess.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := auth.UserID(r.Context())
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		person, err := q.GetDashboardPerson(r.Context(), actor)
		if err != nil {
			http.Error(w, "Votre espace est temporairement indisponible.", 503)
			return
		}
		v := views.DashboardView{SecurityData: pageSecurity(r), SiteName: site, Title: "Mon espace - " + site, Name: person.FirstName + " " + person.LastName}
		v.Memberships, err = q.ListDashboardMemberships(r.Context(), person.ID)
		if err != nil {
			http.Error(w, "Votre espace est temporairement indisponible.", 503)
			return
		}
		children, err := g.ListManagedChildren(r.Context())
		if err != nil {
			http.Error(w, "Votre espace est temporairement indisponible.", 503)
			return
		}
		for _, child := range children {
			memberships, e := q.ListDashboardMemberships(r.Context(), child.PersonID)
			if e != nil {
				http.Error(w, "Votre espace est temporairement indisponible.", 503)
				return
			}
			v.Children = append(v.Children, views.DashboardPerson{Name: child.FirstName + " " + child.LastName, Memberships: memberships})
		}
		var body bytes.Buffer
		if err = views.RenderDashboard(&body, v); err != nil {
			http.Error(w, "Votre espace est temporairement indisponible.", 503)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body.Bytes())
	}
}
func RegisterDashboard(mux *http.ServeMux, site string, q *dbsqlc.Queries, g *guardianaccess.Service, csrf *websecurity.CSRF) {
	mux.Handle("GET /dashboard", RequireAuthenticated(csrf.Protect(DashboardHandler(site, q, g))))
}
