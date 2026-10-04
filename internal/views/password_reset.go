package views

import (
	"embed"
	"html/template"
	"io"
)

//go:embed templates/layouts/base.html templates/pages/password_reset.html
var passwordResetFiles embed.FS
var passwordResetTemplate = template.Must(template.ParseFS(passwordResetFiles, "templates/layouts/base.html", "templates/pages/password_reset.html"))

type PasswordResetData struct {
	SecurityData
	SiteName, Title, Message, Token string
	Reset, Invalid, Error, Sent     bool
}

func RenderPasswordReset(w io.Writer, data PasswordResetData) error {
	return passwordResetTemplate.ExecuteTemplate(w, "base", data)
}
