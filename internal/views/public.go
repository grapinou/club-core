package views

import (
	"embed"
	"fmt"
	"github.com/grapinou/club-core/internal/trials"
	"html/template"
	"io"
	"net/url"
	"time"
)

//go:embed templates/layouts/base.html templates/pages/public.html
var publicFiles embed.FS
var publicTemplate = template.Must(template.New("base").Funcs(template.FuncMap{"publicDate": publicDate, "publicWeekday": publicWeekday}).ParseFS(publicFiles, "templates/layouts/base.html", "templates/pages/public.html"))

var publicDays = []string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}

func publicDate(value string) string {
	d, err := time.Parse("2006-01-02", value)
	if err != nil {
		return value
	}
	return fmt.Sprintf("%s %02d/%02d/%d", publicDays[d.Weekday()], d.Day(), d.Month(), d.Year())
}
func publicWeekday(iso int) string {
	if iso < 1 || iso > 7 {
		return ""
	}
	return publicDays[iso%7]
}

type PublicLocationView struct{ Name, Address string }
type PublicLinkView struct{ Label, URL string }
type PublicImageView struct {
	Src, WebPSrcset, Alt string
	Width, Height        int32
}
type PublicClubView struct {
	Name, ShortName, Description, Email, Phone, PhoneLabel, Website          string
	TrialSessionDescription, TrialEquipmentOffer, TrialEquipmentDetailPrompt string
	TrialItemsToBring                                                        []string
	HeroImage, ActivityImage, CommunityImage, ScheduleImage, TrialImage      PublicImageView
	Locations                                                                []PublicLocationView
	Links                                                                    []PublicLinkView
}
type PublicSlotView struct{ Start, End, Activity, Group, Practice, Location, Address string }
type PublicDayView struct {
	Name  string
	Slots []PublicSlotView
}
type PublicActivityChoice struct {
	ID   int32
	Name string
}
type PublicPage struct {
	SecurityData
	SiteName, Title, Heading, Kind, Editorial string
	Club                                      PublicClubView
	Activities, MembershipTypes               []string
	Season                                    string
	Days                                      []PublicDayView
	Offerings                                 []trials.PublicOffering
	ActivityChoices                           []PublicActivityChoice
	SelectedActivity, SelectedSlot            int32
	SelectedDates                             []string
	Form                                      url.Values
	Errors                                    map[string]string
	Confirmation                              *trials.PublicConfirmation
}

func RenderPublic(w io.Writer, data PublicPage) error {
	return publicTemplate.ExecuteTemplate(w, "base", data)
}

func (p PublicPage) V(key string) string {
	if p.Form == nil {
		return ""
	}
	return p.Form.Get(key)
}
func (p PublicPage) E(key string) string { return p.Errors[key] }
