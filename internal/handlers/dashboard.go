package handlers

import (
	"bytes"
	"net/http"

	"github.com/grapinou/club-core/internal/personalspace"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
)

func DashboardHandler(site string, s *personalspace.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		d, err := s.GetDashboard(r.Context())
		if err != nil {
			personalError(w, r, err)
			return
		}
		v := views.DashboardView{SecurityData: pageSecurity(r), SiteName: site, Title: "Mon espace - " + site, Dashboard: d}
		var body bytes.Buffer
		if err = views.RenderDashboard(&body, v); err != nil {
			personalError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body.Bytes())
	}
}
func RegisterDashboard(mux *http.ServeMux, site string, s *personalspace.Service, csrf *websecurity.CSRF) {
	mux.Handle("GET /dashboard", RequireAuthenticated(csrf.Protect(DashboardHandler(site, s))))
}
