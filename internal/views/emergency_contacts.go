package views

import (
	"embed"
	"html/template"
	"io"
	"net/url"
	"strconv"

	"github.com/grapinou/club-core/internal/emergencycontacts"
)

type EmergencyView struct {
	SecurityData
	emergencycontacts.Page
	SiteName, Title, Path, Back, Error string
	Form                               url.Values
	FailedID                           int32
}

func (v EmergencyView) Field(id int32, key, fallback string) string {
	if v.Error != "" && v.FailedID == id {
		return v.Form.Get(key)
	}
	return fallback
}
func (v EmergencyView) PriorityOptions() []int32 {
	out := []int32{}
	for i := range v.Contacts {
		out = append(out, int32(i+1))
	}
	return out
}
func (v EmergencyView) Rank(id, priority int32) string {
	if v.Error != "" && v.FailedID == id {
		return v.Form.Get("priority")
	}
	return strconv.Itoa(int(priority))
}

//go:embed templates/layouts/base.html templates/pages/emergency_contacts.html
var emergencyFiles embed.FS
var emergencyTemplate = template.Must(template.ParseFS(emergencyFiles, "templates/layouts/base.html", "templates/pages/emergency_contacts.html"))

func RenderEmergency(w io.Writer, v EmergencyView) error {
	return emergencyTemplate.ExecuteTemplate(w, "base", v)
}
