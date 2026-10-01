package views

import (
	"embed"
	"html/template"
	"io"

	"github.com/grapinou/club-core/internal/administration"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

type FamilyView struct {
	SecurityData
	SiteName, Title, Error, Search string
	Person                         administration.Person
	Candidates                     []dbsqlc.SearchAdministrativePersonsRow
}

//go:embed templates/layouts/base.html templates/pages/family.html
var familyFiles embed.FS
var familyTemplate = template.Must(template.ParseFS(familyFiles, "templates/layouts/base.html", "templates/pages/family.html"))

func RenderFamily(w io.Writer, v FamilyView) error {
	return familyTemplate.ExecuteTemplate(w, "base", v)
}
