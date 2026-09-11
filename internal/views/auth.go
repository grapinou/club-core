package views

import (
	"embed"
	"html/template"
	"io"
)

//go:embed templates/layouts/base.html templates/pages/auth.html
var authFiles embed.FS
var authTemplate = template.Must(template.ParseFS(authFiles, "templates/layouts/base.html", "templates/pages/auth.html"))

type AuthData struct {
	SecurityData
	SiteName, Title, Message string
	Activation, LoggedIn     bool
}

func RenderAuth(w io.Writer, data AuthData) error {
	return authTemplate.ExecuteTemplate(w, "base", data)
}
