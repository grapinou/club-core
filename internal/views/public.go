package views

import (
	"embed"
	"html/template"
	"io"
)

//go:embed templates/layouts/base.html templates/pages/public.html
var publicFiles embed.FS
var publicTemplate = template.Must(template.ParseFS(publicFiles, "templates/layouts/base.html", "templates/pages/public.html"))

type PublicLocationView struct{ Name, Address string }
type PublicLinkView struct{ Label, URL string }
type PublicClubView struct {
	Name, ShortName, Description, Email, Phone, Website string
	Locations                                           []PublicLocationView
	Links                                               []PublicLinkView
}
type PublicSlotView struct{ Start, End, Activity, Group, Practice, Location, Address string }
type PublicDayView struct {
	Name  string
	Slots []PublicSlotView
}
type PublicPage struct {
	SecurityData
	SiteName, Title, Heading, Kind, Editorial string
	Club                                      PublicClubView
	Activities, MembershipTypes               []string
	Season                                    string
	Days                                      []PublicDayView
}

func RenderPublic(w io.Writer, data PublicPage) error {
	return publicTemplate.ExecuteTemplate(w, "base", data)
}
