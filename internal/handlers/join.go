package handlers

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/grapinou/club-core/internal/registrationapplications"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
	"github.com/jackc/pgx/v5/pgtype"
)

type JoinHandler struct {
	site         string
	applications *registrationapplications.Service
	limiter      *AttemptLimiter
}

func NewJoinHandler(site string, s *registrationapplications.Service, l *AttemptLimiter) *JoinHandler {
	return &JoinHandler{site, s, l}
}
func (h *JoinHandler) Register(mux *http.ServeMux, csrf *websecurity.CSRF) {
	mux.Handle("GET /join", csrf.Protect(http.HandlerFunc(h.get)))
	mux.Handle("GET /join/submitted", csrf.Protect(http.HandlerFunc(h.submitted)))
	protected := csrf.Protect(http.HandlerFunc(h.post))
	mux.Handle("POST /join", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.limiter.AllowRequest(r) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Retry-After", "900")
			http.Error(w, "Trop de demandes ont été effectuées. Réessayez plus tard.", http.StatusTooManyRequests)
			return
		}
		protected.ServeHTTP(w, r)
	}))
}
func (h *JoinHandler) render(w http.ResponseWriter, r *http.Request, v views.JoinView, status int) {
	v.SecurityData = pageSecurity(r)
	v.SiteName = h.site
	v.Title = "Adhérer - " + h.site
	var buf bytes.Buffer
	if err := views.RenderJoin(&buf, v); err != nil {
		http.Error(w, "Le formulaire est momentanément indisponible.", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}
func (h *JoinHandler) get(w http.ResponseWriter, r *http.Request) {
	c, err := h.applications.Catalog(r.Context())
	if err != nil {
		http.Error(w, "Le formulaire est momentanément indisponible.", 500)
		return
	}
	token, err := h.applications.Present(c, websecurity.Token(r.Context()))
	if err != nil {
		http.Error(w, "Le formulaire est momentanément indisponible.", 500)
		return
	}
	h.render(w, r, views.JoinView{Catalog: c, Presentation: token, Values: url.Values{}, Unavailable: len(c.Seasons) == 0 || len(c.Types) == 0 || len(c.Activities) == 0}, 200)
}
func (h *JoinHandler) submitted(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, views.JoinView{Submitted: true}, 200)
}
func joinInput(form url.Values) (registrationapplications.Input, registrationapplications.ValidationErrors) {
	fields := registrationapplications.ValidationErrors{}
	in := registrationapplications.Input{Presentation: form.Get("presentation")}
	for _, key := range []string{"first_name", "last_name", "birth_date", "email", "phone_number", "address", "season_id", "membership_type_id", "presentation", "action", "csrf_token"} {
		if len(form[key]) > 1 {
			fields["form"] = "Chaque champ doit être renseigné une seule fois."
		}
	}
	text := func(key string) pgtype.Text {
		v := strings.TrimSpace(form.Get(key))
		return pgtype.Text{String: v, Valid: v != ""}
	}
	in.Identity = identityresolution.SubmissionInput{FirstName: strings.TrimSpace(form.Get("first_name")), LastName: strings.TrimSpace(form.Get("last_name")), Email: text("email"), PhoneNumber: text("phone_number"), Address: text("address")}
	if birth, err := time.Parse("2006-01-02", form.Get("birth_date")); err == nil {
		in.Identity.BirthDate = pgtype.Date{Time: birth, Valid: true}
	}
	in.SeasonID, _ = parseID(form.Get("season_id"))
	in.MembershipTypeID, _ = parseID(form.Get("membership_type_id"))
	for _, value := range form["activity_id"] {
		id, err := parseID(value)
		if err != nil {
			fields["activities"] = "Choisissez des activités disponibles."
		}
		in.ActivityIDs = append(in.ActivityIDs, id)
	}
	for key, values := range form {
		if !strings.HasPrefix(key, "consent_") {
			continue
		}
		id, err := parseID(strings.TrimPrefix(key, "consent_"))
		if err != nil || strconv.FormatInt(int64(id), 10) != strings.TrimPrefix(key, "consent_") {
			fields["consents"] = "Un consentement transmis n'est pas valide."
		}
		for _, v := range values {
			in.Consents = append(in.Consents, memberships.Decision{ConsentDefinitionID: id, Decision: v})
		}
	}
	return in, fields
}
func (h *JoinHandler) post(w http.ResponseWriter, r *http.Request) {
	in, fields := joinInput(r.PostForm)
	action := r.PostForm.Get("action")
	if action != "review" && action != "edit" && action != "submit" {
		fields["form"] = "Vérifiez votre demande avant de l'enregistrer."
	}
	var err error
	if len(fields) == 0 && action == "submit" {
		_, err = h.applications.Submit(r.Context(), in, websecurity.Token(r.Context()))
		if err == nil {
			http.Redirect(w, r, "/join/submitted", http.StatusSeeOther)
			return
		}
	} else {
		err = h.applications.Validate(r.Context(), in, websecurity.Token(r.Context()))
	}
	var validation registrationapplications.ValidationErrors
	if errors.As(err, &validation) {
		for k, v := range validation {
			fields[k] = v
		}
	} else if err != nil {
		http.Error(w, "La demande ne peut pas être enregistrée pour le moment. Réessayez plus tard.", 503)
		return
	}
	c, err := h.applications.Catalog(r.Context())
	if err != nil {
		http.Error(w, "Le formulaire est momentanément indisponible.", 503)
		return
	}
	token := in.Presentation
	if presented, err := h.applications.PresentedCatalog(r.Context(), c, token, websecurity.Token(r.Context())); err == nil {
		c = presented
	} else {
		token, err = h.applications.Present(c, websecurity.Token(r.Context()))
		if err != nil {
			http.Error(w, "Le formulaire est momentanément indisponible.", 503)
			return
		}
		for key := range r.PostForm {
			if strings.HasPrefix(key, "consent_") {
				delete(r.PostForm, key)
			}
		}
	}
	v := views.JoinView{Catalog: c, Presentation: token, Values: r.PostForm, Errors: fields, Review: len(fields) == 0 && action == "review"}
	if v.Review {
		for key, values := range r.PostForm {
			if key == "action" {
				continue
			}
			for _, value := range values {
				v.Hidden = append(v.Hidden, views.JoinHidden{Name: key, Value: value})
			}
		}
	}
	status := 200
	if len(fields) > 0 {
		status = 422
	}
	h.render(w, r, v, status)
}
