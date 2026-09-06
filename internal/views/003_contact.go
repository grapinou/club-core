package views

import (
	"embed"
	"html/template"
	"io"
)

//go:embed templates/layouts/base.html
//go:embed templates/pages/003_contact.html
var contactFiles embed.FS

type ContactData struct {
	SiteName     string
	Title        string
	Heading      string
	Description  string
	EmailAddress string
	PhoneNumber  string
}

var contactTemplate = template.Must(
	template.ParseFS(contactFiles,
		"templates/layouts/base.html",
		"templates/pages/003_contact.html"),
)

func RenderContact(w io.Writer, data ContactData) error {
	return contactTemplate.ExecuteTemplate(
		w,
		"base",
		data,
	)
}
