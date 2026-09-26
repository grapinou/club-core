package handlers

import (
	"bytes"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/clubconfig"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type ClubConfigHandler struct {
	site    string
	service *clubconfig.Service
}

func NewClubConfigHandler(site string, s *clubconfig.Service) *ClubConfigHandler {
	return &ClubConfigHandler{site, s}
}
func (h *ClubConfigHandler) Register(mux *http.ServeMux, a *Access, csrf *websecurity.CSRF) {
	protected := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, a.RequirePermission(authorization.ClubConfigure, csrf.Protect(handler)))
	}
	protected("GET /admin/config", h.index)
	protected("GET /admin/config/{section}", h.section)
	protected("GET /admin/config/{section}/{record}", h.edit)
	protected("POST /admin/config/{section}/{record}", h.save)
}
func (h *ClubConfigHandler) view(r *http.Request) views.ClubConfigView {
	return views.ClubConfigView{SecurityData: pageSecurity(r), SiteName: h.site, Title: "Configuration de l’association · " + h.site}
}
func (h *ClubConfigHandler) render(w http.ResponseWriter, v views.ClubConfigView, status int) {
	var b bytes.Buffer
	if err := views.RenderClubConfig(&b, v); err != nil {
		http.Error(w, "Page temporairement indisponible.", 503)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(b.Bytes())
}
func (h *ClubConfigHandler) index(w http.ResponseWriter, r *http.Request) {
	v := h.view(r)
	var err error
	v.Snapshot, err = h.service.Snapshot(r.Context())
	if err != nil {
		http.Error(w, "Configuration temporairement indisponible.", 503)
		return
	}
	h.render(w, v, 200)
}
func (h *ClubConfigHandler) section(w http.ResponseWriter, r *http.Request) {
	v := h.view(r)
	sec, _, err := h.service.Record(r.Context(), r.PathValue("section"), 0)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	v.Section = sec
	if r.URL.Query().Get("saved") == "1" {
		v.Notice = "La modification a été enregistrée."
	}
	h.render(w, v, 200)
}
func configRecordID(r *http.Request) (int32, error) {
	if r.PathValue("record") == "new" {
		return 0, nil
	}
	n, e := strconv.ParseInt(r.PathValue("record"), 10, 32)
	if e != nil || n <= 0 {
		return 0, clubconfig.ErrInvalid
	}
	return int32(n), nil
}
func (h *ClubConfigHandler) edit(w http.ResponseWriter, r *http.Request) {
	id, e := configRecordID(r)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	v := h.view(r)
	v.Editing = true
	v.Section, v.Record, e = h.service.Record(r.Context(), r.PathValue("section"), id)
	if errors.Is(e, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if e != nil {
		http.Error(w, "Configuration temporairement indisponible.", 503)
		return
	}
	if v.Section.Blocked != "" {
		http.Redirect(w, r, "/admin/config/"+v.Section.Key, 303)
		return
	}
	h.render(w, v, 200)
}
func (h *ClubConfigHandler) save(w http.ResponseWriter, r *http.Request) {
	id, e := configRecordID(r)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if e = r.ParseForm(); e != nil {
		http.Error(w, "Formulaire invalide.", 400)
		return
	}
	v := h.view(r)
	v.Editing = true
	v.Section, v.Record, e = h.service.Record(r.Context(), r.PathValue("section"), id)
	if errors.Is(e, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if e != nil {
		http.Error(w, "Configuration temporairement indisponible.", 503)
		return
	}
	if v.Section.Blocked != "" {
		http.Error(w, v.Section.Blocked, 422)
		return
	}
	_, e = h.service.Save(r.Context(), v.Section.Key, id, r.PostForm)
	if e == nil {
		http.Redirect(w, r, "/admin/config/"+v.Section.Key+"?saved=1", 303)
		return
	}
	var dbErr *pgconn.PgError
	status := 422
	switch {
	case errors.Is(e, authorization.ErrForbidden):
		http.Error(w, "Accès refusé", 403)
		return
	case errors.Is(e, clubconfig.ErrDependency):
		v.Error = "Complétez d’abord les étapes précédentes."
	case errors.Is(e, clubconfig.ErrInvalid):
		v.Error = "Vérifiez les champs du formulaire, les dates et les éléments choisis."
	case errors.Is(e, clubconfig.ErrHistorical):
		v.Error = "Cet élément est déjà utilisé. Désactivez-le et créez un nouvel élément pour préserver l’historique."
	case errors.As(e, &dbErr) && dbErr.Code == "23505":
		v.Error = "Cet élément existe déjà."
	case errors.As(e, &dbErr) && strings.HasPrefix(dbErr.Code, "23"):
		v.Error = "Cette modification n’est pas compatible avec les données existantes."
	default:
		status = 503
		v.Error = "La configuration est temporairement indisponible."
	}
	v.Record.Values = map[string]string{}
	for key := range r.PostForm {
		v.Record.Values[key] = r.PostForm.Get(key)
	}
	v.FieldErrors = map[string]string{}
	if r.PostForm.Get("remove") != "yes" {
		for _, field := range v.Section.Fields {
			value := strings.TrimSpace(r.PostForm.Get(field.Name))
			if field.Required && value == "" {
				v.FieldErrors[field.Name] = "Ce champ est obligatoire."
				continue
			}
			if value != "" && (field.Type == "date" || field.Type == "time") {
				layout := "2006-01-02"
				if field.Type == "time" {
					layout = "15:04"
				}
				if _, err := time.Parse(layout, value); err != nil {
					v.FieldErrors[field.Name] = "Vérifiez cette valeur."
				}
			}
		}
		if v.Section.Key == "horaires" && r.PostForm.Get("end_time") <= r.PostForm.Get("start_time") && r.PostForm.Get("end_time") != "" {
			v.FieldErrors["end_time"] = "L’heure de fin doit suivre l’heure de début."
		}
		if v.Section.Key == "saisons" && r.PostForm.Get("ends_at") < r.PostForm.Get("starts_at") && r.PostForm.Get("ends_at") != "" {
			v.FieldErrors["ends_at"] = "La fin doit suivre le début."
		}
	}
	h.render(w, v, status)
}
