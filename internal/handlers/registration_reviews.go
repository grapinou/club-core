package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/grapinou/club-core/internal/accounts"
	"net/http"
	"time"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/grapinou/club-core/internal/registrationapplications"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
)

type RegistrationHandler struct {
	applications *registrationapplications.Service
	site         string
	loc          *time.Location
	reviews      *identityresolution.ReviewService
}

func NewRegistrationHandler(site string, loc *time.Location, reviews *identityresolution.ReviewService, applications *registrationapplications.Service) *RegistrationHandler {
	return &RegistrationHandler{applications, site, loc, reviews}
}
func (h *RegistrationHandler) Register(mux *http.ServeMux, access *Access, csrf *websecurity.CSRF) {
	for _, route := range []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"POST /registration-reviews/{id}/guardian-activation", h.guardianActivation}, {"POST /registration-reviews/{id}/confirm-guardian", h.confirmGuardian}, {"POST /registration-reviews/{id}/link-guardian", h.linkGuardian}, {"POST /registration-reviews/{id}/create-guardian", h.createGuardian}, {"POST /registration-reviews/{id}/finalize-application", h.finalize}, {"GET /registration-reviews", h.list}, {"GET /registration-reviews/{id}", h.detail}, {"POST /registration-reviews/{id}/link-person", h.link}, {"POST /registration-reviews/{id}/create-person", h.create},
	} {
		mux.Handle(route.path, access.RequirePermission(authorization.RegistrationsReview, csrf.Protect(route.handler)))
	}
}
func (h *RegistrationHandler) list(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.UserID(r.Context())
	rows, err := h.reviews.List(r.Context(), actor)
	if err != nil {
		membershipError(w, r, err)
		return
	}
	v := views.RegistrationListView{SecurityData: pageSecurity(r), SiteName: h.site, Title: "Vérifications - " + h.site, Rows: views.RegistrationRows(rows, h.loc)}
	var buf bytes.Buffer
	err = views.RenderRegistrationList(&buf, v)
	writeMembershipPage(w, &buf, err)
}
func (h *RegistrationHandler) detail(w http.ResponseWriter, r *http.Request) {
	id, ok := membershipID(w, r)
	if !ok {
		return
	}
	actor, _ := auth.UserID(r.Context())
	d, err := h.reviews.GetDetails(r.Context(), actor, id)
	if err != nil {
		membershipError(w, r, err)
		return
	}
	v := views.RegistrationDetail(d, h.loc)
	v.SecurityData = pageSecurity(r)
	v.SiteName = h.site
	v.Title = "Vérification d'identité - " + h.site
	switch r.URL.Query().Get("notice") {
	case "activation_failed":
		v.Notice = "Activation non envoyée. Le dossier est conservé ; vous pouvez réessayer."
	case "activation_prepared":
		v.Notice = "Activation guardian préparée. Vérifiez l’état du compte et la disponibilité de l’envoi email."
	case "resolved":
		v.Notice = "Résolution d'identité enregistrée."
	case "closed":
		v.Notice = "Cette soumission a déjà été traitée. Aucune nouvelle résolution n'a été effectuée."
	}
	var buf bytes.Buffer
	err = views.RenderRegistrationDetail(&buf, v)
	writeMembershipPage(w, &buf, err)
}
func (h *RegistrationHandler) link(w http.ResponseWriter, r *http.Request)   { h.resolve(w, r, true) }
func (h *RegistrationHandler) create(w http.ResponseWriter, r *http.Request) { h.resolve(w, r, false) }
func (h *RegistrationHandler) resolve(w http.ResponseWriter, r *http.Request, link bool) {
	id, ok := membershipID(w, r)
	if !ok {
		return
	}
	actor, _ := auth.UserID(r.Context())
	var err error
	if link {
		person, parseErr := parseID(r.PostForm.Get("person_id"))
		if parseErr != nil || person <= 0 {
			http.NotFound(w, r)
			return
		}
		err = h.reviews.LinkPerson(r.Context(), actor, id, person)
	} else {
		err = h.reviews.CreatePerson(r.Context(), actor, id)
	}
	notice := "resolved"
	if errors.Is(err, identityresolution.ErrClosed) {
		notice = "closed"
	} else if err != nil {
		membershipError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/registration-reviews/%d?notice=%s", id, notice), http.StatusSeeOther)
}

func (h *RegistrationHandler) finalize(w http.ResponseWriter, r *http.Request) {
	id, ok := membershipID(w, r)
	if !ok {
		return
	}
	actor, _ := auth.UserID(r.Context())
	if err := h.reviews.RetryApplication(r.Context(), actor, id); err != nil {
		membershipError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/registration-reviews/%d", id), http.StatusSeeOther)
}

func (h *RegistrationHandler) confirmGuardian(w http.ResponseWriter, r *http.Request) {
	id, ok := membershipID(w, r)
	if !ok {
		return
	}
	if err := h.applications.ConfirmGuardian(r.Context(), id); err != nil {
		membershipError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/registration-reviews/%d", id), http.StatusSeeOther)
}
func (h *RegistrationHandler) linkGuardian(w http.ResponseWriter, r *http.Request) {
	h.resolveGuardian(w, r, true)
}
func (h *RegistrationHandler) createGuardian(w http.ResponseWriter, r *http.Request) {
	h.resolveGuardian(w, r, false)
}
func (h *RegistrationHandler) resolveGuardian(w http.ResponseWriter, r *http.Request, link bool) {
	id, ok := membershipID(w, r)
	if !ok {
		return
	}
	actor, _ := auth.UserID(r.Context())
	var person *int32
	if link {
		p, err := parseID(r.PostForm.Get("person_id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		person = &p
	}
	err := h.reviews.ResolveGuardian(r.Context(), actor, id, person)
	if err != nil && !errors.Is(err, identityresolution.ErrClosed) {
		membershipError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/registration-reviews/%d", id), http.StatusSeeOther)
}

func (h *RegistrationHandler) guardianActivation(w http.ResponseWriter, r *http.Request) {
	id, ok := membershipID(w, r)
	if !ok {
		return
	}
	err := h.applications.RetryGuardianActivation(r.Context(), id)
	notice := "activation_prepared"
	var delivery *accounts.DeliveryError
	if errors.As(err, &delivery) {
		notice = "activation_failed"
	} else if err != nil {
		membershipError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/registration-reviews/%d?notice=%s", id, notice), http.StatusSeeOther)
}
