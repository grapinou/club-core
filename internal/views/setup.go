package views

import (
	"embed"
	"html/template"
	"io"
)

type SetupView struct {
	SecurityData
	SiteName, Title, Mode, Error         string
	FirstName, LastName, Username, Email string
}

//go:embed templates/layouts/base.html templates/pages/setup.html
var setupFiles embed.FS
var setupTemplate = template.Must(template.ParseFS(setupFiles, "templates/layouts/base.html", "templates/pages/setup.html"))

func RenderSetup(w io.Writer, v SetupView) error { return setupTemplate.ExecuteTemplate(w, "base", v) }
