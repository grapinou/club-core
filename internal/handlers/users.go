package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/useraccess"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
	"github.com/jackc/pgx/v5"
)

type UsersHandler struct {
	site    string
	service *useraccess.Service
}

func NewUsersHandler(site string, service *useraccess.Service) *UsersHandler {
	return &UsersHandler{site, service}
}
func (h *UsersHandler) Register(mux *http.ServeMux, access *Access, csrf *websecurity.CSRF) {
	mux.Handle("GET /admin/users", access.RequirePermission(authorization.RolesRead, csrf.Protect(http.HandlerFunc(h.list))))
	mux.Handle("GET /admin/users/{id}", access.RequirePermission(authorization.RolesRead, csrf.Protect(http.HandlerFunc(h.detail))))
	mux.Handle("POST /admin/users/{id}/roles", access.RequirePermission(authorization.RolesManage, csrf.Protect(http.HandlerFunc(h.change))))
}
func (h *UsersHandler) base(r *http.Request, mode string) views.UsersView {
	return views.UsersView{SecurityData: pageSecurity(r), SiteName: h.site, Title: "Utilisateurs - " + h.site, Mode: mode}
}
func (h *UsersHandler) render(w http.ResponseWriter, v views.UsersView, status int) {
	var b bytes.Buffer
	if err := views.RenderUsers(&b, v); err != nil {
		http.Error(w, "Page temporairement indisponible.", 503)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(b.Bytes())
}
func userID(r *http.Request) (int32, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 32)
	if err != nil || id <= 0 {
		return 0, useraccess.ErrInvalid
	}
	return int32(id), nil
}
func (h *UsersHandler) list(w http.ResponseWriter, r *http.Request) {
	v := h.base(r, "list")
	v.Search = r.URL.Query().Get("q")
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || page < 0 || page > 10000 {
			http.Error(w, "Page invalide", 400)
			return
		}
		v.Page = int32(page)
	}
	users, err := h.service.List(r.Context(), v.Search, v.Page)
	if errors.Is(err, useraccess.ErrInvalid) {
		http.Error(w, "Recherche invalide", 400)
		return
	}
	if err != nil {
		http.Error(w, "Utilisateurs temporairement indisponibles", 503)
		return
	}
	if len(users) > 50 {
		v.More = true
		users = users[:50]
	}
	v.Users = users
	h.render(w, v, 200)
}
func (h *UsersHandler) detail(w http.ResponseWriter, r *http.Request) {
	id, err := userID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	v := h.base(r, "detail")
	v.User, err = h.service.Get(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Compte temporairement indisponible", 503)
		return
	}
	for _, role := range []string{"secretary", "treasurer", "coach", "president"} {
		v.Roles = append(v.Roles, views.RoleOption{Name: role, Label: views.RoleLabel(role), Assigned: v.User.HasRole(role)})
	}
	if r.URL.Query().Get("saved") == "1" {
		v.Notice = "Le rôle a été mis à jour."
	}
	if r.URL.Query().Get("error") == "last" {
		v.Error = "Impossible de retirer ce rôle : aucun autre compte ne pourrait ensuite gérer les accès administratifs."
	}
	h.render(w, v, 200)
}
func (h *UsersHandler) change(w http.ResponseWriter, r *http.Request) {
	id, err := userID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	action := r.PostForm.Get("action")
	if action != "add" && action != "remove" {
		http.Error(w, "Action invalide", 422)
		return
	}
	err = h.service.Change(r.Context(), id, r.PostForm.Get("role"), action == "add")
	if errors.Is(err, useraccess.ErrLastManager) {
		http.Redirect(w, r, fmt.Sprintf("/admin/users/%d?error=last", id), 303)
		return
	}
	if errors.Is(err, authorization.ErrForbidden) {
		http.Error(w, "Accès refusé", 403)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, useraccess.ErrInvalid) {
		http.Error(w, "Rôle invalide", 422)
		return
	}
	if err != nil {
		http.Error(w, "Modification temporairement indisponible", 503)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/users/%d?saved=1", id), 303)
}
