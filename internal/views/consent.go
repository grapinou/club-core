package views

import (
	"embed"
	"html/template"
	"io"
)

type ConsentChoice struct {
	ID   int32
	Name string
}
type ConsentPage struct {
	SecurityData
	SiteName, Title, Action, Back, Error, ConsentTitle, Description, Decision string
	Active, Granted, Office                                                   bool
	Givers                                                                    []ConsentChoice
}

//go:embed templates/layouts/base.html templates/pages/consent.html
var consentFiles embed.FS
var consentTemplate = template.Must(template.ParseFS(consentFiles, "templates/layouts/base.html", "templates/pages/consent.html"))

func RenderConsent(w io.Writer, v ConsentPage) error {
	return consentTemplate.ExecuteTemplate(w, "base", v)
}
