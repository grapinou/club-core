package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"database/sql"

	"github.com/grapinou/club-core/internal/accounts"
	"github.com/grapinou/club-core/internal/administration"
	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/consents"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/database/dbtypes"
	"github.com/grapinou/club-core/internal/guardianaccess"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/grapinou/club-core/internal/trials"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
	"modernc.org/sqlite"
)

type AdministrativeHandler struct {
	site string
	s    *administration.Service
	p    PermissionChecker
}

func NewAdministrativeHandler(site string, s *administration.Service, p PermissionChecker) *AdministrativeHandler {
	return &AdministrativeHandler{site, s, p}
}
func (h *AdministrativeHandler) base(r *http.Request, mode string) views.AdministrativeView {
	id, _ := auth.UserID(r.Context())
	write, _ := h.p.HasPermission(r.Context(), id, authorization.MembershipsApprove)
	title := map[string]string{"home": "Mon tableau de bord", "people": "Annuaire", "person": "Coordonnées et parcours", "trials": "Essais", "trial": "Dossier d’essai", "trial-form": "Programmer un essai", "trial-person": "Programmer un essai", "trial-repeat": "Refaire un essai", "membership-new": "Demander une adhésion", "membership-person": "Créer une adhésion", "membership-groups": "Groupes de l’adhésion", "membership-notes": "Notes de l’adhésion"}[mode]
	return views.AdministrativeView{SecurityData: pageSecurity(r), SiteName: h.site, Title: title + " - " + h.site, Mode: mode, Today: h.s.Today(), Form: url.Values{}, CanManageMemberships: write}
}
func (h *AdministrativeHandler) render(w http.ResponseWriter, r *http.Request, v views.AdministrativeView, err error) {
	status := 200
	if err != nil {
		var dbErr *sqlite.Error
		var quota *trials.QuotaExceededError
		switch {
		case errors.As(err, &quota):
			status = 422
			v.Error = fmt.Sprintf("Cette personne a atteint le nombre maximal de séances d’essai pour cette saison (%d sur %d).", quota.Used, quota.Limit)
		case errors.Is(err, trials.ErrQuotaSeason):
			status = 422
			v.Error = "La saison des essais ne peut pas être déterminée sans ambiguïté. Choisissez un créneau ou vérifiez les dates et les saisons avant d’ajouter une place au quota."
		case errors.Is(err, authorization.ErrForbidden):
			http.Error(w, "Accès refusé", 403)
			return
		case errors.Is(err, sql.ErrNoRows):
			http.NotFound(w, r)
			return
		case errors.Is(err, administration.ErrConflict):
			status = 409
			v.Error = "Ce dossier a été modifié. Rechargez la page avant de réessayer."
		case errors.Is(err, trials.ErrConverted):
			status = 422
			v.Error = "Cet essai est à l’origine d’une adhésion. Sa séance est conservée ; créez un nouvel essai pour une autre séance."
		case errors.Is(err, guardianaccess.ErrIneligible) || errors.Is(err, accounts.ErrDisabled):
			status = 422
			v.Error = "Vérifiez la relation avec le responsable, l’âge de l’enfant, l’autorisation d’accès et l’état du compte."
		case errors.Is(err, administration.ErrInvalid) || errors.Is(err, trials.ErrInvalidSchedule) || errors.Is(err, memberships.ErrInvalidRequest) || errors.Is(err, consents.ErrInvalidDecision) || errors.Is(err, consents.ErrUnauthorizedGiver):
			status = 422
			v.Error = "Vérifiez les informations du formulaire."
			switch v.Mode {
			case "person", "membership-notes":
				v.Error = "Les notes doivent contenir au maximum 10 000 caractères."
			case "trial":
				switch {
				case strings.HasSuffix(r.URL.Path, "/status"):
					v.Error = "Choisissez un résultat d’essai valide."
				case strings.HasSuffix(r.URL.Path, "/notes"):
					v.Error = "Les notes de l’essai doivent contenir au maximum 10 000 caractères."
				default:
					v.Error = "Choisissez une activité et un groupe actifs correspondants, puis une date compatible avec le jour et la période du créneau."
				}
			case "trial-form":
				v.Error = "Choisissez une activité et un groupe actifs correspondants, puis une date compatible avec le jour et la période du créneau."
			case "membership-new":
				v.Error = "Vérifiez la date de naissance, la saison, le type, les activités et les décisions recueillies. L’essai d’origine doit être présent et correspondre à la personne et à une activité choisie."
			case "membership-groups":
				v.Error = "Le groupe doit être actif, compatible avec le type d’adhésion et correspondre à une activité de l’adhésion. Vérifiez les dates et l’absence de chevauchement avec l’historique."
			}
		case errors.As(err, &dbErr) && (dbErr.Code() == 2067 || dbErr.Code() == 275 || dbErr.Code() == 787):
			status = 422
			v.Error = "Cette opération est incompatible avec le dossier ou existe déjà. Consultez son historique."
		default:
			status = 503
			v.Error = "L’administration est temporairement indisponible."
		}
	}
	if r.URL.Query().Get("saved") == "1" && err == nil {
		v.Notice = "La modification a été enregistrée."
	}
	if r.URL.Query().Get("family_no_channel") == "1" && err == nil {
		v.Error = "Le compte du responsable est préparé, mais son email doit être renseigné pour envoyer l’activation."
	}
	if r.URL.Query().Get("family_send_failed") == "1" && err == nil {
		v.Error = "Le compte du responsable est préparé, mais l’email n’a pas pu être envoyé."
	}
	var b bytes.Buffer
	if e := views.RenderAdministrative(&b, v); e != nil {
		http.Error(w, "Page temporairement indisponible.", 503)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(b.Bytes())
}
func adminID(r *http.Request, key string) (int32, error) {
	id, e := strconv.ParseInt(r.PathValue(key), 10, 32)
	if e != nil || id <= 0 {
		return 0, sql.ErrNoRows
	}
	return int32(id), nil
}
func formID(f url.Values, key string, optional bool) (int32, error) {
	if optional && f.Get(key) == "" {
		return 0, nil
	}
	n, e := strconv.ParseInt(f.Get(key), 10, 32)
	if e != nil || n <= 0 {
		return 0, administration.ErrInvalid
	}
	return int32(n), nil
}
func formDate(value string, optional bool) (dbtypes.Date, error) {
	if value == "" && optional {
		return dbtypes.Date{}, nil
	}
	t, e := time.Parse("2006-01-02", value)
	if e != nil {
		return dbtypes.Date{}, administration.ErrInvalid
	}
	return dbtypes.Date{Time: t, Valid: true}, nil
}
func scheduleForm(f url.Values) (dbsqlc.RescheduleTrialParams, error) {
	var p dbsqlc.RescheduleTrialParams
	var e error
	p.ActivityID, e = formID(f, "activity_id", false)
	if e != nil {
		return p, e
	}
	g, e := formID(f, "group_id", true)
	if e != nil {
		return p, e
	}
	slot, e := formID(f, "slot_id", true)
	if e != nil {
		return p, e
	}
	p.GroupID = sql.NullInt32{Int32: g, Valid: g != 0}
	p.GroupSlotID = sql.NullInt32{Int32: slot, Valid: slot != 0}
	p.TrialDate, e = formDate(f.Get("trial_date"), false)
	return p, e
}
func adminRedirect(w http.ResponseWriter, r *http.Request, path string) {
	http.Redirect(w, r, path+"?saved=1", 303)
}
func (h *AdministrativeHandler) Register(mux *http.ServeMux, a *Access, csrf *websecurity.CSRF) {
	reg := func(pattern string, p authorization.Permission, fn http.HandlerFunc) {
		mux.Handle(pattern, a.RequirePermission(p, csrf.Protect(fn)))
	}
	reg("GET /memberships/new", authorization.MembershipsApprove, h.membershipPerson)
	reg("POST /memberships/new", authorization.MembershipsApprove, h.membershipPerson)
	reg("GET /admin", authorization.PersonsRead, h.home)
	for _, path := range []string{"/members", "/guardians", "/prospects"} {
		reg("GET "+path, authorization.PersonsRead, h.People)
		reg("POST "+path, authorization.PersonsRead, h.People)
	}
	reg("POST /persons/search", authorization.PersonsRead, h.People)
	reg("GET /persons/{id}", authorization.PersonsRead, h.person)
	reg("POST /persons/{id}/notes", authorization.PersonsWrite, h.person)
	reg("GET /trials", authorization.PersonsRead, h.listTrials)
	reg("GET /trials/new", authorization.PersonsWrite, h.trialPerson)
	reg("GET /trials/new/repeat", authorization.PersonsWrite, h.repeatTrialPerson)
	reg("GET /trials/{id}", authorization.PersonsRead, h.trial)
	for _, action := range []string{"reschedule", "status", "notes"} {
		reg("POST /trials/{id}/"+action, authorization.PersonsWrite, h.trial)
	}
	reg("GET /persons/{id}/trials/new", authorization.PersonsWrite, h.schedule)
	reg("POST /persons/{id}/trials/new", authorization.PersonsWrite, h.schedule)
	reg("GET /persons/{id}/memberships/new", authorization.MembershipsApprove, h.requestMembership)
	reg("POST /persons/{id}/memberships/new", authorization.MembershipsApprove, h.requestMembership)
	reg("GET /memberships/{id}/groups", authorization.MembershipsRead, h.groups)
	reg("POST /memberships/{id}/groups", authorization.MembershipsApprove, h.groups)
	reg("POST /memberships/{id}/groups/{assignment}/close", authorization.MembershipsApprove, h.groups)
	reg("GET /memberships/{id}/notes", authorization.MembershipsApprove, h.membershipNotes)
	reg("POST /memberships/{id}/notes", authorization.MembershipsApprove, h.membershipNotes)
}
func (h *AdministrativeHandler) home(w http.ResponseWriter, r *http.Request) {
	v := h.base(r, "home")
	var e error
	v.Home, e = h.s.Dashboard(r.Context())
	v.SetMembershipAttention(v.Home.Memberships, h.s.Location())
	h.render(w, r, v, e)
}
func (h *AdministrativeHandler) People(w http.ResponseWriter, r *http.Request) {
	v := h.base(r, "people")
	v.Category = r.URL.Query().Get("category")
	page := int32(0)
	var e error
	if raw := r.URL.Query().Get("page"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || n < 0 || n > 10000 {
			e = administration.ErrInvalid
		} else {
			page = int32(n)
		}
	}
	if r.Method == "POST" {
		v.Search = r.PostForm.Get("search")
		v.Category = r.PostForm.Get("category")
		page = 0
	}
	v.ListPath = "/persons"
	switch r.URL.Path {
	case "/members":
		v.Category, v.ListPath = "members", "/members"
	case "/guardians":
		v.Category, v.ListPath = "guardians", "/guardians"
	case "/prospects":
		v.Category, v.ListPath = "prospects", "/prospects"
	}
	v.SearchPath = v.ListPath
	if v.ListPath == "/persons" {
		v.SearchPath = "/persons/search"
	}
	v.Title = v.PeopleTitle() + " - " + h.site
	if e == nil {
		v.People, e = h.s.People(r.Context(), v.Search, page, v.Category)
	}
	if len(v.People) > 50 {
		v.More = true
		v.People = v.People[:50]
		if v.Search == "" {
			v.NextURL = fmt.Sprintf("%s?category=%s&page=%d", v.ListPath, url.QueryEscape(v.Category), page+1)
		}
	}
	if page > 0 {
		v.PreviousURL = fmt.Sprintf("%s?category=%s&page=%d", v.ListPath, url.QueryEscape(v.Category), page-1)
	}
	h.render(w, r, v, e)
}
func (h *AdministrativeHandler) person(w http.ResponseWriter, r *http.Request) {
	v := h.base(r, "person")
	id, e := adminID(r, "id")
	if e == nil {
		v.Person, e = h.s.Person(r.Context(), id)
	}
	v.Form.Set("notes", v.Person.Info.Notes.String)
	if e == nil && r.Method == "POST" {
		v.Form = r.PostForm
		e = h.s.UpdatePersonNotes(r.Context(), id, v.Form.Get("notes"))
		if e == nil {
			adminRedirect(w, r, fmt.Sprintf("/persons/%d", id))
			return
		}
	}
	h.render(w, r, v, e)
}
func (h *AdministrativeHandler) listTrials(w http.ResponseWriter, r *http.Request) {
	v := h.base(r, "trials")
	v.Form = r.URL.Query()
	v.Search = strings.TrimSpace(v.Form.Get("search"))
	// Existing direct date links also open management, with its visible filters.
	v.TrialManagement = v.Form.Get("all") == "1" || v.Form.Get("date") != "" || v.Search != ""
	var e error
	v.TrialPolicy, e = h.s.TrialPolicy(r.Context())
	if e != nil {
		h.render(w, r, v, e)
		return
	}
	if v.TrialManagement {
		v.Title = "Gestion des essais - " + h.site
		on, err := formDate(v.Form.Get("date"), true)
		e = err
		var page int32
		if raw := v.Form.Get("page"); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 32)
			if err != nil || n < 0 || n > 10000 {
				e = administration.ErrInvalid
			} else {
				page = int32(n)
			}
		}
		if e == nil {
			v.Trials, e = h.s.SearchTrials(r.Context(), on, v.Search, page)
		}
		link := func(p int32) string {
			return "/trials?" + url.Values{"all": {"1"}, "date": {v.Form.Get("date")}, "search": {v.Search}, "page": {fmt.Sprint(p)}}.Encode()
		}
		if len(v.Trials) > 100 {
			v.Trials = v.Trials[:100]
			v.NextURL = link(page + 1)
		}
		if page > 0 {
			v.PreviousURL = link(page - 1)
		}
	} else {
		start := h.s.Today().Time
		if raw := v.Form.Get("week"); raw != "" {
			date, err := formDate(raw, false)
			start, e = date.Time, err
		}
		if e == nil {
			start = start.AddDate(0, 0, -(int(start.Weekday())+6)%7)
			v.Trials, e = h.s.TrialsInWeek(r.Context(), start)
			if len(v.Trials) > 100 {
				v.More = true
				v.Trials = v.Trials[:100]
			}
			v.SetWeek(start)
		}
	}
	h.render(w, r, v, e)
}
func fillTrial(v *views.AdministrativeView) {
	t := v.Trial
	v.Form.Set("trial_date", t.TrialDate.Time.Format("2006-01-02"))
	v.Form.Set("activity_id", fmt.Sprint(t.ActivityID))
	if t.GroupID.Valid {
		v.Form.Set("group_id", fmt.Sprint(t.GroupID.Int32))
	}
	if t.GroupSlotID.Valid {
		v.Form.Set("slot_id", fmt.Sprint(t.GroupSlotID.Int32))
	}
	v.Form.Set("revision", fmt.Sprint(t.Revision))
	v.Form.Set("status", t.Status)
	v.Form.Set("notes", t.Notes.String)
}
func (h *AdministrativeHandler) trial(w http.ResponseWriter, r *http.Request) {
	v := h.base(r, "trial")
	id, e := adminID(r, "id")
	if e == nil {
		v.Trial, e = h.s.Trial(r.Context(), id)
	}
	if e == nil {
		v.TrialQuota, e = h.s.TrialQuota(r.Context(), id)
	}
	if e == nil {
		v.TrialHistory, e = h.s.PersonTrials(r.Context(), v.Trial.PersonID)
	}
	if e == nil {
		var relations []dbsqlc.AdministrativeRelationsRow
		relations, e = h.s.Relations(r.Context(), v.Trial.PersonID)
		for _, relation := range relations {
			if relation.IsGuardian {
				v.TrialGuardians = append(v.TrialGuardians, relation)
			}
		}
	}
	if e == nil {
		v.Choices, e = h.s.Choices(r.Context())
	}
	fillTrial(&v)
	if e == nil && r.Method == "POST" {
		for key, values := range r.PostForm {
			v.Form[key] = values
		}
		revision, err := strconv.ParseInt(v.Form.Get("revision"), 10, 32)
		e = err
		if e != nil || revision < 0 {
			e = administration.ErrInvalid
		}
		action := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		p := dbsqlc.RescheduleTrialParams{}
		if e == nil && action == "reschedule" {
			p, e = scheduleForm(v.Form)
		}
		value := v.Form.Get(action)
		if e == nil {
			e = h.s.UpdateTrial(r.Context(), id, int32(revision), action, p, value)
		}
		if e == nil {
			adminRedirect(w, r, fmt.Sprintf("/trials/%d", id))
			return
		}
	}
	h.render(w, r, v, e)
}
func (h *AdministrativeHandler) schedule(w http.ResponseWriter, r *http.Request) {
	v := h.base(r, "trial-form")
	v.CurrentPath = "/trials/new"
	id, e := adminID(r, "id")
	if e == nil {
		v.Person, e = h.s.Person(r.Context(), id)
	}
	if e == nil {
		v.Choices, e = h.s.Choices(r.Context())
	}
	if e == nil {
		v.Person.TrialQuotas, e = h.s.PersonTrialQuotas(r.Context(), id)
	}
	v.Form.Set("trial_date", h.s.Today().Time.Format("2006-01-02"))
	if e == nil && r.URL.Query().Has("from_trial") {
		values := r.URL.Query()["from_trial"]
		sourceID, parseErr := strconv.ParseInt(r.URL.Query().Get("from_trial"), 10, 32)
		if parseErr != nil || sourceID <= 0 || len(values) != 1 {
			e = administration.ErrInvalid
		} else {
			var source dbsqlc.AdministrativeTrialsRow
			source, e = h.s.Trial(r.Context(), int32(sourceID))
			if e == nil && source.PersonID != id {
				e = sql.ErrNoRows
			}
			if e == nil && r.Method == "GET" {
				v.PrefillRepeatTrial(source)
			}
		}
	}
	if e == nil && r.Method == "POST" {
		v.Form = r.PostForm
		var p dbsqlc.RescheduleTrialParams
		p, e = scheduleForm(v.Form)
		if e == nil {
			var trial int32
			trial, e = h.s.Schedule(r.Context(), dbsqlc.CreateTrialParams{PersonID: id, ActivityID: p.ActivityID, GroupID: p.GroupID, GroupSlotID: p.GroupSlotID, TrialDate: p.TrialDate, Notes: sql.NullString{String: v.Form.Get("notes"), Valid: true}})
			if e == nil {
				adminRedirect(w, r, fmt.Sprintf("/trials/%d", trial))
				return
			}
		}
	}
	h.render(w, r, v, e)
}
func (h *AdministrativeHandler) membershipPerson(w http.ResponseWriter, r *http.Request) {
	v := h.base(r, "membership-person")
	// This entry requires both creation and person-read capabilities, including GET.
	actor, _ := auth.UserID(r.Context())
	allowed, err := h.p.HasPermission(r.Context(), actor, authorization.PersonsRead)
	if err != nil {
		h.render(w, r, v, err)
		return
	}
	if !allowed {
		h.render(w, r, v, authorization.ErrForbidden)
		return
	}
	if r.Method == "POST" {
		v.Search = strings.TrimSpace(r.PostForm.Get("search"))
	}
	if v.Search != "" {
		v.People, err = h.s.People(r.Context(), v.Search, 0, "")
	}
	if len(v.People) > 50 {
		v.More = true
		v.People = v.People[:50]
	}
	h.render(w, r, v, err)
}
func (h *AdministrativeHandler) requestMembership(w http.ResponseWriter, r *http.Request) {
	v := h.base(r, "membership-new")
	// The existing Person-based route belongs to the membership workflow.
	v.CurrentPath = "/memberships/new"
	id, e := adminID(r, "id")
	if e == nil {
		v.Person, e = h.s.Person(r.Context(), id)
	}
	if e == nil {
		v.Choices, e = h.s.Choices(r.Context())
	}
	if e == nil && r.Method == "GET" && r.URL.Query().Get("trial") != "" {
		source, err := formID(r.URL.Query(), "trial", false)
		e = err
		if e == nil {
			t, err := h.s.Trial(r.Context(), source)
			e = err
			if e == nil && t.PersonID != id {
				e = sql.ErrNoRows
			}
			if e == nil {
				if t.MembershipID != 0 {
					http.Redirect(w, r, fmt.Sprintf("/memberships/%d", t.MembershipID), http.StatusSeeOther)
					return
				}
				v.Trial = t
				v.Form.Set("source_trial", fmt.Sprint(source))
				v.Form.Set("activities", fmt.Sprint(t.ActivityID))
				if t.SuggestedSeasonID != 0 {
					v.Form.Set("season_id", fmt.Sprint(t.SuggestedSeasonID))
				}
			}
		}
	}
	if e == nil && r.Method == "POST" && r.PostForm.Get("action") != "choices" {
		v.Form = r.PostForm
		req := memberships.Request{PersonID: id}
		// Recover the selected context before validating editable fields, so an
		// error never presents a sourced request as a direct membership.
		source, sourceErr := formID(v.Form, "source_trial", true)
		e = sourceErr
		if e == nil && source != 0 {
			v.Trial, e = h.s.Trial(r.Context(), source)
			if e == nil && v.Trial.PersonID != id {
				v.Trial = dbsqlc.AdministrativeTrialsRow{}
				e = memberships.ErrInvalidRequest
			}
		}
		if e == nil {
			req.SeasonID, e = formID(v.Form, "season_id", false)
		}
		if e == nil {
			req.MembershipTypeID, e = formID(v.Form, "type_id", false)
		}
		for _, raw := range v.Form["activities"] {
			if e != nil {
				break
			}
			n, err := formID(url.Values{"id": {raw}}, "id", false)
			e = err
			req.ActivityIDs = append(req.ActivityIDs, n)
		}
		for _, raw := range v.Form["definitions"] {
			if e != nil {
				break
			}
			n, err := formID(url.Values{"id": {raw}}, "id", false)
			e = err
			var giver int32
			if e == nil {
				giver, e = formID(v.Form, "giver_id", false)
			}
			req.Consents = append(req.Consents, memberships.Decision{ConsentDefinitionID: n, GivenByPersonID: giver, Decision: v.Form.Get("consent-" + raw)})
		}
		if e == nil {
			var membership int32
			groups := map[int32]int32{}
			for _, activity := range req.ActivityIDs {
				group, err := formID(v.Form, fmt.Sprintf("group-%d", activity), true)
				if err != nil {
					e = err
					break
				}
				if source != 0 && activity == v.Trial.ActivityID && group != 0 {
					e = memberships.ErrInvalidRequest
					break
				}
				groups[activity] = group
			}
			if e == nil {
				membership, e = h.s.RequestMembership(r.Context(), req, source, groups)
			}
			if e == nil {
				redirectMembership(w, r, membership, "requested")
				return
			}
		}
	}
	if e == nil && r.Method == "POST" && r.PostForm.Get("action") == "choices" {
		v.Form = r.PostForm
		source, parseErr := formID(v.Form, "source_trial", true)
		e = parseErr
		if e == nil && source != 0 {
			v.Trial, e = h.s.Trial(r.Context(), source)
			if e == nil && v.Trial.PersonID != id {
				e = sql.ErrNoRows
			}
		}
	}
	if e == nil && v.Trial.ID == 0 {
		v.EligibleTrials, e = h.s.EligibleSourceTrials(r.Context(), id)
	}
	// Existing dossiers stay linked; occupied seasons cannot be selected again.
	available := v.Choices.Seasons[:0]
	for _, season := range v.Choices.Seasons {
		occupied := false
		for _, m := range v.Person.Memberships {
			occupied = occupied || m.SeasonID == season.ID
		}
		if !occupied {
			available = append(available, season)
		}
	}
	v.Choices.Seasons = available
	h.render(w, r, v, e)
}
func (h *AdministrativeHandler) groups(w http.ResponseWriter, r *http.Request) {
	v := h.base(r, "membership-groups")
	id, e := adminID(r, "id")
	if e == nil {
		v.Membership, e = h.s.Membership(r.Context(), id)
	}
	if e == nil {
		v.Choices, e = h.s.Choices(r.Context())
	}
	if e == nil {
		filtered := v.Choices.Groups[:0]
		for _, g := range v.Choices.Groups {
			for _, activity := range v.Membership.ActivityIDs {
				if activity == g.ActivityID && v.GroupCompatible(g.ID) {
					filtered = append(filtered, g)
					break
				}
			}
		}
		v.Choices.Groups = filtered

		if v.Membership.DefaultJoined.Valid {
			v.Form.Set("joined_at", v.Membership.DefaultJoined.Time.Format("2006-01-02"))
		}
	}
	if e == nil && r.Method == "POST" {
		v.Form = r.PostForm
		if r.PathValue("assignment") != "" {
			var assignment int32
			assignment, e = adminID(r, "assignment")
			var left dbtypes.Date
			if e == nil {
				left, e = formDate(v.Form.Get("left_at"), false)
			}
			if e == nil {
				e = h.s.CloseGroup(r.Context(), id, assignment, left)
			}
		} else {
			var g int32
			g, e = formID(v.Form, "group_id", true)
			var joined dbtypes.Date
			if e == nil {
				joined, e = formDate(v.Form.Get("joined_at"), false)
			}
			if e == nil && g != 0 {
				e = h.s.AssignGroup(r.Context(), dbsqlc.AssignMembershipGroupParams{MembershipID: id, GroupID: g, JoinedAt: joined})
			}
		}
		if e == nil {
			adminRedirect(w, r, fmt.Sprintf("/memberships/%d/groups", id))
			return
		}
	}
	h.render(w, r, v, e)
}
func (h *AdministrativeHandler) membershipNotes(w http.ResponseWriter, r *http.Request) {
	v := h.base(r, "membership-notes")
	id, e := adminID(r, "id")
	if e == nil {
		v.Membership, e = h.s.Membership(r.Context(), id)
	}
	v.Form.Set("notes", v.Membership.Info.AdminNote.String)
	if e == nil && r.Method == "POST" {
		v.Form = r.PostForm
		e = h.s.UpdateMembershipNotes(r.Context(), id, v.Form.Get("notes"))
		if e == nil {
			adminRedirect(w, r, fmt.Sprintf("/memberships/%d/notes", id))
			return
		}
	}
	h.render(w, r, v, e)
}

// trialPerson orchestrates the existing Person and scheduling flows.
func (h *AdministrativeHandler) trialPerson(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, h.base(r, "trial-person"), nil)
}

func (h *AdministrativeHandler) repeatTrialPerson(w http.ResponseWriter, r *http.Request) {
	v := h.base(r, "trial-repeat")
	v.Search = strings.TrimSpace(r.URL.Query().Get("search"))
	var page int32
	var err error
	if raw := r.URL.Query().Get("page"); raw != "" {
		n, e := strconv.ParseInt(raw, 10, 32)
		if e != nil || n < 0 || n > 10000 {
			err = administration.ErrInvalid
		} else {
			page = int32(n)
		}
	}
	if err == nil {
		v.RepeatPeople, err = h.s.RepeatTrialPersons(r.Context(), v.Search, page)
	}
	link := func(p int32) string {
		return "/trials/new/repeat?" + url.Values{"search": {v.Search}, "page": {fmt.Sprint(p)}}.Encode()
	}
	if len(v.RepeatPeople) > 50 {
		v.RepeatPeople = v.RepeatPeople[:50]
		v.NextURL = link(page + 1)
	}
	if page > 0 {
		v.PreviousURL = link(page - 1)
	}
	h.render(w, r, v, err)
}
