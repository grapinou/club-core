package handlers

import (
	"net/http"

	"github.com/grapinou/club-manager/internal/config"
	"github.com/grapinou/club-manager/internal/views"
)

func ContactHandler(cfg config.Config) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		data := views.ContactData{
			SiteName:     cfg.SiteName,
			Title:        cfg.Contact.Title + " - " + cfg.SiteName,
			Heading:      cfg.Contact.Heading,
			Description:  cfg.Contact.Description,
			EmailAddress: cfg.Contact.EmailAddress,
			PhoneNumber:  cfg.Contact.PhoneNumber,
		}

		err := views.RenderContact(w, data)

		if err != nil {
			http.Error(
				w,
				"Erreur interne du serveur",
				http.StatusInternalServerError,
			)
		}
	}

}
