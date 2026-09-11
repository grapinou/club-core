package handlers

import (
	"bytes"
	"context"
	"net/http"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
)

type PermissionChecker interface {
	HasPermission(context.Context, int32, authorization.Permission) (bool, error)
}
type Access struct {
	checker  PermissionChecker
	siteName string
}

func NewAccess(siteName string, checker PermissionChecker) *Access {
	return &Access{checker: checker, siteName: siteName}
}
func RequireAuthenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if _, ok := auth.UserID(r.Context()); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *Access) RequirePermission(permission authorization.Permission, next http.Handler) http.Handler {
	return RequireAuthenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := auth.UserID(r.Context())
		allowed, err := a.checker.HasPermission(r.Context(), id, permission)
		if err != nil {
			http.Error(w, "Erreur interne du serveur", 500)
			return
		}
		if !allowed {
			var body bytes.Buffer
			data := views.PageData{SiteName: a.siteName, Title: "Accès refusé - " + a.siteName, Heading: "Accès refusé", Description: "Vous ne disposez pas des autorisations nécessaires pour accéder à cette page."}
			if err := views.RenderPage(&body, data); err != nil {
				http.Error(w, "Accès refusé", 403)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write(body.Bytes())
			return
		}
		next.ServeHTTP(w, r)
	}))
}

type navigationKey struct{}
type membershipNavigationKey struct{}

func (a *Access) Navigation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := auth.UserID(r.Context()); ok {
			// Per-request display hint; errors hide the link and grant no permissions.
			allowed, err := a.checker.HasPermission(r.Context(), id, authorization.PersonsRead)
			if err == nil {
				r = r.WithContext(context.WithValue(r.Context(), navigationKey{}, allowed))
			}
			membershipRead, membershipErr := a.checker.HasPermission(r.Context(), id, authorization.MembershipsRead)
			if membershipErr == nil {
				r = r.WithContext(context.WithValue(r.Context(), membershipNavigationKey{}, membershipRead))
			}
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
func pageSecurity(r *http.Request) views.SecurityData {
	canRead, _ := r.Context().Value(navigationKey{}).(bool)
	canReadMemberships, _ := r.Context().Value(membershipNavigationKey{}).(bool)
	return views.SecurityData{CanReadMemberships: canReadMemberships, CanReadPersons: canRead, CSRFToken: websecurity.Token(r.Context())}
}
