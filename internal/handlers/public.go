package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grapinou/club-core/internal/mailer"
	"github.com/grapinou/club-core/internal/organization"
	"github.com/grapinou/club-core/internal/trials"
	"github.com/grapinou/club-core/internal/views"
)

type PublicHandler struct {
	service  *organization.Service
	location *time.Location
	rules    string
	trials   *trials.PublicService
	limiter  *AttemptLimiter
	sender   mailer.Mailer
	mailFrom string
}

func NewPublicHandler(s *organization.Service, loc *time.Location, rules string, trialService *trials.PublicService, limiter *AttemptLimiter, sender mailer.Mailer, mailFrom string) *PublicHandler {
	return &PublicHandler{service: s, location: loc, rules: rules, trials: trialService, limiter: limiter, sender: sender, mailFrom: mailFrom}
}
func (h *PublicHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && h.limiter != nil && !h.limiter.AllowRequest(r) {
		w.Header().Set("Retry-After", "900")
		http.Error(w, "Trop de demandes ont été effectuées. Réessayez plus tard.", http.StatusTooManyRequests)
		return
	}

	titles := map[string]string{"/": "Accueil", "/horaires": "Horaires", "/tarifs": "Adhésions et tarifs", "/contact": "Contact", "/essai": "Réserver un essai", "/rules": "Règlement intérieur"}
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
	if c.RulesDescription != "" {
		data.Editorial = c.RulesDescription
	}
	if data.SiteName == "" {
		data.SiteName = "Club Core"
	}
	if r.URL.Path == "/essai" {
		if err = h.trialPage(r, &data, c); err != nil {
			http.Error(w, "Les créneaux sont momentanément indisponibles.", 503)
			return
		}
	}
	data.Title = title + " · " + data.SiteName
	data.MetaDescription = title + " : découvrez les informations pratiques et contactez " + data.SiteName + "."
	data.Club = views.PublicClubView{MaxTrialsPerPersonPerSeason: c.MaxTrialsPerPersonPerSeason, Name: c.Name, ShortName: c.ShortName, Description: c.Description, Email: c.Email, Phone: c.Phone, PhoneLabel: c.PhoneLabel, Website: c.Website,
		TrialSessionDescription: c.TrialSessionDescription, TrialEquipmentOffer: c.TrialEquipmentOffer, TrialEquipmentDetailPrompt: c.TrialEquipmentDetailPrompt, TrialItemsToBring: c.TrialItemsToBring}
	imageView := func(key string) views.PublicImageView {
		i := c.Images[key]
		return views.PublicImageView{Src: i.Src, WebPSrcset: i.WebPSrcset, Alt: i.Alt, Width: i.Width, Height: i.Height}
	}
	data.Club.HeroImage = imageView("hero")
	data.Club.ActivityImage = imageView("activity")
	data.Club.CommunityImage = imageView("community")
	data.Club.ScheduleImage = imageView("schedule")
	data.Club.TrialImage = imageView("trial")
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
			data.Prices, err = h.service.PublicPrices(r.Context())
		case "/horaires":
			var schedule organization.PublicTimetable
			schedule, err = h.service.PublicSchedule(r.Context(), time.Now().In(h.location))
			data.Season = schedule.Season
			data.HasSlots = len(schedule.Slots) > 0
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
	if len(data.Errors) > 0 {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}
	_, _ = w.Write(body.Bytes())
}

func (h *PublicHandler) trialPage(r *http.Request, data *views.PublicPage, club organization.PublicClub) error {
	data.Form = url.Values{}
	if r.Method == http.MethodPost {
		data.Form = r.PostForm
	} else {
		data.Form = r.URL.Query()
	}
	offerings, err := h.trials.Offerings(r.Context(), time.Now().In(h.location))
	if err != nil {
		return err
	}
	data.Offerings = offerings
	seen := map[int32]bool{}
	for _, o := range offerings {
		if !seen[o.ActivityID] {
			data.ActivityChoices = append(data.ActivityChoices, views.PublicActivityChoice{ID: o.ActivityID, Name: o.Activity})
			seen[o.ActivityID] = true
		}
	}
	activity, _ := strconv.ParseInt(data.Form.Get("activity"), 10, 32)
	slot, _ := strconv.ParseInt(data.Form.Get("slot"), 10, 32)
	data.SelectedActivity = int32(activity)
	data.SelectedSlot = int32(slot)
	for _, o := range offerings {
		if o.ActivityID == data.SelectedActivity && o.SlotID == data.SelectedSlot {
			data.SelectedDates = o.Dates
			break
		}
	}
	if r.Method != http.MethodPost {
		return nil
	}
	data.Errors = map[string]string{}
	b := trials.PublicBooking{Offering: trials.PublicOffering{ActivityID: data.SelectedActivity, SlotID: data.SelectedSlot}, Date: data.Form.Get("date"), FirstName: data.Form.Get("first_name"), LastName: data.Form.Get("last_name"), BirthDate: data.Form.Get("birth_date"), Email: data.Form.Get("email"), Phone: data.Form.Get("phone"), Minor: data.Form.Get("minor") == "yes", GuardianFirstName: data.Form.Get("guardian_first_name"), GuardianLastName: data.Form.Get("guardian_last_name"), GuardianEmail: data.Form.Get("guardian_email"), GuardianPhone: data.Form.Get("guardian_phone"), Relationship: data.Form.Get("relationship"), EquipmentNeeded: data.Form.Get("equipment_needed") == "yes", EquipmentDetails: data.Form.Get("equipment_details")}
	for _, o := range offerings {
		if o.ActivityID == b.Offering.ActivityID && o.SlotID == b.Offering.SlotID {
			b.Offering.GroupID = o.GroupID
			break
		}
	}
	if b.Offering.GroupID == 0 {
		data.Errors["form"] = "La séance choisie n’est plus disponible. Choisissez un autre créneau."
		data.Errors["slot"] = "Choisissez une séance proposée."
	}
	found := false
	for _, d := range data.SelectedDates {
		if d == b.Date {
			found = true
			break
		}
	}
	if !found {
		data.Errors["date"] = "Choisissez une date proposée pour cette séance."
	}
	if club.TrialEquipmentOffer != "" && data.Form.Get("equipment_needed") != "yes" && data.Form.Get("equipment_needed") != "no" {
		data.Errors["equipment_needed"] = "Indiquez si du matériel est nécessaire."
	}
	if b.EquipmentNeeded && club.TrialEquipmentDetailPrompt != "" && strings.TrimSpace(b.EquipmentDetails) == "" {
		data.Errors["equipment_details"] = "Ajoutez la précision demandée par le club."
	}
	if len(b.EquipmentDetails) > 500 {
		data.Errors["equipment_details"] = "La précision est trop longue."
	}
	for _, field := range []string{"first_name", "last_name", "birth_date"} {
		if strings.TrimSpace(data.Form.Get(field)) == "" {
			data.Errors[field] = "Ce champ est nécessaire."
		}
	}
	birth, birthErr := time.Parse("2006-01-02", b.BirthDate)
	if birthErr != nil || birth.After(time.Now().In(h.location)) {
		data.Errors["birth_date"] = "Indiquez une date de naissance valide."
	} else {
		today := time.Now().In(h.location)
		isMinor := birth.After(today.AddDate(-18, 0, 0))
		if isMinor != b.Minor {
			data.Errors["birth_date"] = "Vérifiez la date de naissance et le choix adulte ou mineur."
		}
	}
	checkEmail := func(field string) {
		value := strings.TrimSpace(data.Form.Get(field))
		if value == "" {
			return
		}
		address, err := mail.ParseAddress(value)
		if err != nil || address.Address != value || len(value) > 254 {
			data.Errors[field] = "Indiquez un email valide."
		}
	}
	if b.Minor {
		for _, field := range []string{"guardian_first_name", "guardian_last_name", "guardian_email", "guardian_phone", "relationship"} {
			if strings.TrimSpace(data.Form.Get(field)) == "" {
				data.Errors[field] = "Ce champ est nécessaire."
			}
		}
		checkEmail("guardian_email")
	} else {
		for _, field := range []string{"email", "phone"} {
			if strings.TrimSpace(data.Form.Get(field)) == "" {
				data.Errors[field] = "Ce champ est nécessaire."
			}
		}
		checkEmail("email")
	}
	if len(data.Errors) > 0 {
		return nil
	}
	confirmation, err := h.trials.Book(r.Context(), b, time.Now().In(h.location))
	if errors.Is(err, trials.ErrInvalidPublicBooking) || errors.Is(err, trials.ErrInvalidSchedule) {
		data.Errors["form"] = "Vérifiez les informations et la séance choisie. Le créneau a peut-être changé."
		return nil
	}
	if err != nil {
		return err
	}
	data.Confirmation = &confirmation
	if h.sender != nil && h.mailFrom != "" {
		message, messageErr := mailer.TrialConfirmationMessage(h.mailFrom, confirmation.EmailTo, mailer.TrialConfirmationData{Club: club.Name, Registrant: confirmation.Registrant.FirstName + " " + confirmation.Registrant.LastName, Activity: confirmation.Offering.Activity, Group: confirmation.Offering.Group, Practice: confirmation.Offering.Practice, Date: confirmation.Date, Start: confirmation.Offering.Start, End: confirmation.Offering.End, Location: confirmation.Offering.Location, Address: confirmation.Offering.Address, SessionDescription: club.TrialSessionDescription, ItemsToBring: strings.Join(club.TrialItemsToBring, ", "), EquipmentOffer: club.TrialEquipmentOffer, EquipmentNeeded: confirmation.EquipmentNeeded, EquipmentDetails: confirmation.EquipmentDetails})
		if messageErr == nil {
			mailCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = h.sender.Send(mailCtx, message)
			cancel()
		}
	}
	return nil
}
