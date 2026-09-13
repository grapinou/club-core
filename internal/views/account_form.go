package views

import (
	"embed"
	"html/template"
	"io"
)

type AccountFormView struct {
	SecurityData
	SiteName, Title, Kind, Phone, Address, Email, Error, Message string
	Errors                                                       map[string]string
}

//go:embed templates/layouts/base.html templates/pages/account_form.html
var accountFormFiles embed.FS
var accountFormTemplate = template.Must(template.ParseFS(accountFormFiles, "templates/layouts/base.html", "templates/pages/account_form.html"))

func RenderAccountForm(w io.Writer, v AccountFormView) error {
	return accountFormTemplate.ExecuteTemplate(w, "base", v)
}
