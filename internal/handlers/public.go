package handlers

import (
	"bytes"
	"net/http"
	"time"

	"github.com/grapinou/club-core/internal/organization"
	"github.com/grapinou/club-core/internal/views"
)

type PublicHandler struct {
	service  *organization.Service
	location *time.Location
	rules    string
}

func NewPublicHandler(s *organization.Service, loc *time.Location, rules string) *PublicHandler {
	return &PublicHandler{s, loc, rules}
}
func (h *PublicHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	titles := map[string]string{"/": "Accueil", "/horaires": "Horaires", "/tarifs": "Adhésions et tarifs", "/contact": "Contact", "/essai": "Faire un essai", "/rules": "Règlement intérieur"}
	title, ok := titles[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	c, err := h.service.PublicIdentity(r.Context())
	if err != nil {
		http.Error(w, "Les informations du club sont momentanément indisponibles.", 503)
		return
	}
	data := views.PublicPage{SecurityData: pageSecurity(r), Kind: r.URL.Path, Heading: title, SiteName: c.Name, Editorial: h.rules}
	if data.SiteName == "" {
		data.SiteName = "Club Core"
	}
	data.Title = title + " · " + data.SiteName
	data.MetaDescription = title + " : découvrez les informations pratiques et contactez " + data.SiteName + "."
	data.Club = views.PublicClubView{Name: c.Name, ShortName: c.ShortName, Description: c.Description, Email: c.Email, Phone: c.Phone, Website: c.Website}
	for _, l := range c.Locations {
		data.Club.Locations = append(data.Club.Locations, views.PublicLocationView{Name: l.Name, Address: l.Address})
	}
	for _, l := range c.Links {
		data.Club.Links = append(data.Club.Links, views.PublicLinkView{Label: l.Label, URL: l.URL})
	}
	if c.Name != "" {
		switch r.URL.Path {
		case "/":
			data.Heading = c.Name
			data.Activities, err = h.service.PublicActivities(r.Context())
		case "/tarifs":
			data.MembershipTypes, err = h.service.PublicMembershipTypes(r.Context())
		case "/horaires":
			var schedule organization.PublicTimetable
			schedule, err = h.service.PublicSchedule(r.Context(), time.Now().In(h.location))
			data.Season = schedule.Season
			days := []string{"Lundi", "Mardi", "Mercredi", "Jeudi", "Vendredi", "Samedi", "Dimanche"}
			for _, name := range days {
				data.Days = append(data.Days, views.PublicDayView{Name: name})
			}
			for _, slot := range schedule.Slots {
				i := int(slot.Weekday) - 1
				data.Days[i].Slots = append(data.Days[i].Slots, views.PublicSlotView{Start: slot.Start, End: slot.End, Activity: slot.Activity, Group: slot.Group, Practice: slot.Practice, Location: slot.Location, Address: slot.Address})
			}
		}
	}
	if err != nil {
		http.Error(w, "Les informations du club sont momentanément indisponibles.", 503)
		return
	}
	var body bytes.Buffer
	if err = views.RenderPublic(&body, data); err != nil {
		http.Error(w, "Erreur interne du serveur", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body.Bytes())
}
