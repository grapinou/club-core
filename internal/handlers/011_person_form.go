package handlers

import (
	"net/http"

	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/views"
)

func PersonFormHandler(cfg config.Config) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		data := views.PersonFormData{SecurityData: pageSecurity(r),
			SiteName:        cfg.SiteName,
			AfterTrial:      r.URL.Query().Get("after") == "trial",
			AfterMembership: r.URL.Query().Get("after") == "membership",
			Title:           "Ajouter une personne - " + cfg.SiteName,
		}

		if data.AfterTrial {
			data.CurrentPath = "/trials/new"
		}
		if data.AfterMembership {
			data.CurrentPath = "/memberships/new"
		}

		err := views.RenderPersonForm(w, data)

		if err != nil {
			http.Error(
				w,
				"Erreur interne du serveur",
				http.StatusInternalServerError,
			)
			return
		}
	}

}
