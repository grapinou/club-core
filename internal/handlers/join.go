package handlers

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbtypes"
	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/grapinou/club-core/internal/registrationapplications"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
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
	mux.Handle("GET /join/child", csrf.Protect(http.HandlerFunc(h.get)))
	mux.Handle("GET /join/child/submitted", csrf.Protect(http.HandlerFunc(h.submitted)))
	mux.Handle("GET /join", csrf.Protect(http.HandlerFunc(h.get)))
	mux.Handle("GET /join/submitted", csrf.Protect(http.HandlerFunc(h.submitted)))
	protected := csrf.Protect(http.HandlerFunc(h.post))
	post := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.limiter.AllowRequest(r) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Retry-After", "900")
			http.Error(w, "Trop de demandes ont été effectuées. Réessayez plus tard.", http.StatusTooManyRequests)
			return
		}
		protected.ServeHTTP(w, r)
	})
	mux.Handle("POST /join", post)
	mux.Handle("POST /join/child", post)
	for _, path := range []string{"/me/children/new", "/me/children/{childID}/join"} {
		mux.Handle("GET "+path, RequireAuthenticated(csrf.Protect(http.HandlerFunc(h.get))))
		mux.Handle("POST "+path, RequireAuthenticated(csrf.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor, _ := auth.UserID(r.Context())
			// Revisions do not spend the budget; family members sharing a network
			// do not consume the anonymous public IP budget.
			if r.PostForm.Get("action") == "submit" && !h.limiter.Allow(fmt.Sprintf("family:%d", actor)) {
				w.Header().Set("Retry-After", "900")
				http.Error(w, "Trop de demandes ont été effectuées. Réessayez plus tard.", http.StatusTooManyRequests)
				return
			}
			h.post(w, r)
		}))))
	}
	mux.Handle("GET /me/children/new/submitted", RequireAuthenticated(csrf.Protect(http.HandlerFunc(h.submitted))))
}
func (h *JoinHandler) render(w http.ResponseWriter, r *http.Request, v views.JoinView, status int) {
	v.Family = strings.HasPrefix(r.URL.Path, "/me/children/")
	v.Child = v.Family || strings.HasPrefix(r.URL.Path, "/join/child")
	v.KnownChild = r.PathValue("childID") != ""
	if v.Family {
		v.ActionPath = strings.TrimSuffix(r.URL.Path, "/submitted")
		if v.Values == nil {
			v.Values = url.Values{}
		}
		if err := h.familyValues(r, v.Values); err != nil {
			membershipError(w, r, err)
			return
		}
	}
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
	values := url.Values{}
	values.Set("season_id", r.URL.Query().Get("season"))
	if err := h.familyValues(r, values); err != nil {
		membershipError(w, r, err)
		return
	}
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
	h.render(w, r, views.JoinView{Catalog: c, Presentation: token, Values: values, Unavailable: len(c.Seasons) == 0 || len(c.Types) == 0 || len(c.Activities) == 0}, 200)
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
	text := func(key string) sql.NullString {
		v := strings.TrimSpace(form.Get(key))
		return sql.NullString{String: v, Valid: v != ""}
	}
	in.Identity = identityresolution.SubmissionInput{FirstName: strings.TrimSpace(form.Get("first_name")), LastName: strings.TrimSpace(form.Get("last_name")), Email: text("email"), PhoneNumber: text("phone_number"), Address: text("address")}
	if birth, err := time.Parse("2006-01-02", form.Get("birth_date")); err == nil {
		in.Identity.BirthDate = dbtypes.Date{Time: birth, Valid: true}
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
	if err := h.familyValues(r, r.PostForm); err != nil {
		membershipError(w, r, err)
		return
	}
	family := strings.HasPrefix(r.URL.Path, "/me/children/")
	child, _ := parseID(r.PathValue("childID"))
	in, fields := joinInput(r.PostForm)
	if r.URL.Path == "/join/child" || family {
		guardianForm := url.Values{}
		for _, key := range []string{"first_name", "last_name", "birth_date", "email", "phone_number", "address"} {
			guardianForm[key] = r.PostForm["guardian_"+key]
			if len(guardianForm[key]) > 1 {
				fields["form"] = "Chaque champ doit être renseigné une seule fois."
			}
		}
		guardian, _ := joinInput(guardianForm)
		if guardianForm.Get("birth_date") != "" && !guardian.Identity.BirthDate.Valid {
			fields["guardian_birth_date"] = "Indiquez une date valide ou laissez ce champ vide."
		}
		for _, key := range []string{"relationship_type", "emergency_contact"} {
			if len(r.PostForm[key]) != 1 {
				fields[key] = "Choisissez une réponse."
			}
		}
		emergency := r.PostForm.Get("emergency_contact")
		if emergency != "yes" && emergency != "no" {
			fields["emergency_contact"] = "Choisissez Oui ou Non."
		}
		in.Child = &registrationapplications.ChildInput{Guardian: guardian.Identity, RelationshipType: r.PostForm.Get("relationship_type"), EmergencyContactRequested: emergency == "yes"}
	}
	action := r.PostForm.Get("action")
	if action != "review" && action != "edit" && action != "submit" {
		fields["form"] = "Vérifiez votre demande avant de l'enregistrer."
	}
	var err error
	if len(fields) == 0 && action == "submit" {
		if family && child != 0 {
			var membership int32
			membership, err = h.applications.SubmitManagedChild(r.Context(), in, websecurity.Token(r.Context()), child)
			if err == nil {
				http.Redirect(w, r, fmt.Sprintf("/me/children/%d/memberships/%d", child, membership), http.StatusSeeOther)
				return
			}
		} else if family {
			_, err = h.applications.SubmitFamily(r.Context(), in, websecurity.Token(r.Context()))
		} else {
			_, err = h.applications.Submit(r.Context(), in, websecurity.Token(r.Context()))
		}
		if err == nil {
			path := "/join/submitted"
			if in.Child != nil {
				path = "/join/child/submitted"
			}
			if family {
				path = "/me/children/new/submitted"
			}
			http.Redirect(w, r, path, http.StatusSeeOther)
			return
		}
	} else {
		if family {
			err = h.applications.ValidateFamily(r.Context(), in, websecurity.Token(r.Context()), child)
		} else {
			err = h.applications.Validate(r.Context(), in, websecurity.Token(r.Context()))
		}
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
			if key == "action" || (family && (strings.HasPrefix(key, "guardian_") || key == "person_id" || key == "child_person_id")) {
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

func (h *JoinHandler) familyValues(r *http.Request, values url.Values) error {
	if !strings.HasPrefix(r.URL.Path, "/me/children/") {
		return nil
	}
	child := int32(0)
	if raw := r.PathValue("childID"); raw != "" {
		var err error
		child, err = parseID(raw)
		if err != nil || child <= 0 {
			return authorization.ErrForbidden
		}
	}
	f, err := h.applications.FamilyIdentity(r.Context(), child)
	if err != nil {
		return err
	}
	fill := func(prefix string, p identityresolution.SubmissionInput) {
		values.Set(prefix+"first_name", p.FirstName)
		values.Set(prefix+"last_name", p.LastName)
		values.Set(prefix+"birth_date", "")
		if p.BirthDate.Valid {
			values.Set(prefix+"birth_date", p.BirthDate.Time.Format("2006-01-02"))
		}
		values.Set(prefix+"email", p.Email.String)
		values.Set(prefix+"phone_number", p.PhoneNumber.String)
		values.Set(prefix+"address", p.Address.String)
	}
	fill("guardian_", f.Guardian)
	if f.Child != nil {
		fill("", *f.Child)
		values.Set("relationship_type", f.Relationship)
		values.Set("emergency_contact", "no")
	}
	return nil
}
