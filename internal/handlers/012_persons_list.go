package handlers

import (
	"net/http"

	"github.com/grapinou/club-manager/internal/config"
	"github.com/grapinou/club-manager/internal/database"
	"github.com/grapinou/club-manager/internal/views"
)

func PersonsListHandler(cfg config.Config, queries database.PersonQueries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		persons, err := queries.ListPersons(r.Context())
		if err != nil {
			http.Error(
				w, "impossible de récupérer la liste des personnes", http.StatusInternalServerError,
			)
			return
		}

		var personData []views.PersonData

		for _, person := range persons {

			email, phoneNumber, address := "Non renseigné", "Non renseigné", "Non renseigné"

			if person.Email.Valid {
				email = person.Email.String
			}

			if person.PhoneNumber.Valid {
				phoneNumber = person.PhoneNumber.String
			}

			if person.Address.Valid {
				address = person.Address.String
			}

			personData = append(personData, views.PersonData{
				ID:          person.ID,
				FirstName:   person.FirstName,
				LastName:    person.LastName,
				BirthDate:   person.BirthDate.Time.Format("02/01/2006"),
				Email:       email,
				PhoneNumber: phoneNumber,
				Address:     address,
				CreatedAt:   person.CreatedAt.Time.Format("02/01/2006 15:04"),
			})
		}

		data := views.PersonsData{
			SiteName: cfg.SiteName,
			Title:    "Personnes - " + cfg.SiteName,
			Persons:  personData,
		}

		if err := views.RenderPersonsList(w, data); err != nil {
			http.Error(
				w, "impossible d'afficher la liste des personnes", http.StatusInternalServerError,
			)
			return
		}
	}
}
