package views

import (
	"embed"
	"html/template"
	"io"
)

//go:embed templates/layouts/base.html templates/pages/registration_verify.html
var registrationVerifyFiles embed.FS
var registrationVerifyTemplate = template.Must(template.ParseFS(registrationVerifyFiles, "templates/layouts/base.html", "templates/pages/registration_verify.html"))

type RegistrationVerifyView struct {
	SecurityData
	SiteName, Title, Message string
}

func RenderRegistrationVerify(w io.Writer, v RegistrationVerifyView) error {
	return registrationVerifyTemplate.ExecuteTemplate(w, "base", v)
}
