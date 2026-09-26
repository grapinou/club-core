package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/grapinou/club-core/internal/accounts"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/guardianaccess"
	"github.com/grapinou/club-core/internal/websecurity"
)

// RegisterFamilyDossier exposes existing resource-scoped services to the office.
func (h *AdministrativeHandler) RegisterFamilyDossier(mux *http.ServeMux, access *Access, csrf *websecurity.CSRF, guardians *guardianaccess.Service, account *accounts.Service) {
	for _, action := range []string{"emergency", "grant", "revoke", "activation"} {
		mux.Handle("POST /persons/{id}/guardians/{guardian}/"+action, access.RequirePermission(authorization.PersonsWrite, csrf.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			child, err := adminID(r, "id")
			guardian, e := adminID(r, "guardian")
			if err == nil {
				err = e
			}
			notice := "saved"
			if err == nil {
				switch action {
				case "emergency":
					err = h.s.AddGuardianEmergency(r.Context(), child, guardian)
				case "grant":
					_, err = guardians.Grant(r.Context(), child, guardian)
				case "revoke":
					err = guardians.Revoke(r.Context(), child, guardian)
				case "activation":
					var result accounts.ResendResult
					result, err = account.EnsureGuardianUser(r.Context(), child, guardian)
					if result.DeliveryStatus == accounts.NoChannel {
						notice = "family_no_channel"
					}
				}
			}
			var delivery *accounts.DeliveryError
			if errors.As(err, &delivery) {
				notice, err = "family_send_failed", nil
			}
			if err != nil {
				v := h.base(r, "person")
				v.Person, _ = h.s.Person(r.Context(), child)
				v.Trials = v.Person.Trials
				h.render(w, r, v, err)
				return
			}
			http.Redirect(w, r, fmt.Sprintf("/persons/%d?%s=1", child, notice), http.StatusSeeOther)
		}))))
	}
}
