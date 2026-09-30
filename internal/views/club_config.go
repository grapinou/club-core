package views

import (
	"embed"
	"html/template"
	"io"

	"github.com/grapinou/club-core/internal/clubconfig"
)

//go:embed templates/layouts/base.html templates/pages/club_config.html
var clubConfigFiles embed.FS
var clubConfigTemplate = template.Must(template.ParseFS(clubConfigFiles, "templates/layouts/base.html", "templates/pages/club_config.html"))

type ClubConfigView struct {
	SecurityData
	SiteName, Title, Error, Notice string
	FieldErrors                    map[string]string
	Snapshot                       clubconfig.Snapshot
	Section                        clubconfig.Section
	Record                         clubconfig.Record
	Editing                        bool
}

func RenderClubConfig(w io.Writer, v ClubConfigView) error {
	return clubConfigTemplate.ExecuteTemplate(w, "base", v)
}
