package views

import (
	"embed"
	"html/template"
	"io"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

type DashboardPerson struct {
	Name        string
	Memberships []dbsqlc.ListDashboardMembershipsRow
}
type DashboardView struct {
	SecurityData
	SiteName, Title, Name string
	Memberships           []dbsqlc.ListDashboardMembershipsRow
	Children              []DashboardPerson
}

//go:embed templates/layouts/base.html templates/pages/dashboard.html
var dashboardFiles embed.FS
var dashboardTemplate = template.Must(template.ParseFS(dashboardFiles, "templates/layouts/base.html", "templates/pages/dashboard.html"))

func RenderDashboard(w io.Writer, v DashboardView) error {
	return dashboardTemplate.ExecuteTemplate(w, "base", v)
}
