package handlers

import (
	"net/http"

	"github.com/grapinou/club-manager/internal/config"
	"github.com/grapinou/club-manager/internal/database"
	"github.com/grapinou/club-manager/internal/views"
)

func UpdatePersonFormHandler(cfg config.Config, queries database.PersonQueries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		id, err := parseID(r.PathValue("id"))
		if err != nil {
			http.Error(w, "id incorrect", http.StatusBadRequest)
			return
		}

		rawPersonData, err := queries.GetPersonByID(r.Context(), id)
		if err != nil {
			http.Error(
				w,
				"impossible de récupérer la personne",
				http.StatusInternalServerError,
			)
			return
		}

		phoneNumber := ""
		email := ""
		address := ""

		if rawPersonData.PhoneNumber.Valid {
			phoneNumber = rawPersonData.PhoneNumber.String
		}

		if rawPersonData.Email.Valid {
			email = rawPersonData.Email.String
		}

		if rawPersonData.Address.Valid {
			address = rawPersonData.Address.String
		}

		person := views.PersonUpdateFormData{
			ID:          rawPersonData.ID,
			FirstName:   rawPersonData.FirstName,
			LastName:    rawPersonData.LastName,
			BirthDate:   rawPersonData.BirthDate.Time.Format("2006-01-02"),
			PhoneNumber: phoneNumber,
			Email:       email,
			Address:     address,
		}

		data := views.PersonUpdateFormPageData{
			SiteName: cfg.SiteName,
			Title:    "Modifier une personne - " + cfg.SiteName,
			Person:   person,
		}

		err = views.RenderPersonUpdateForm(w, data)
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
