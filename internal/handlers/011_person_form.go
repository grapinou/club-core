package handlers

import (
	"net/http"

	"github.com/grapinou/club-manager/internal/config"
	"github.com/grapinou/club-manager/internal/views"
)

func PersonFormHandler(cfg config.Config) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		data := views.PersonFormData{
			SiteName: cfg.SiteName,
			Title:    "Ajouter une personne - " + cfg.SiteName,
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
