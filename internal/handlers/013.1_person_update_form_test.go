package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-manager/internal/config"
	"github.com/grapinou/club-manager/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestUpdatePersonFormHandler(t *testing.T) {

	cfg := config.Config{
		SiteName: "test sur update form",
	}

	request := httptest.NewRequest(http.MethodGet, "/persons/1/edit", nil)

	request.SetPathValue("id", "1")

	response := httptest.NewRecorder()

	queries := &recordingPersonQueries{
		PersonByID: dbsqlc.Person{
			ID:        1,
			FirstName: "Robin",
			LastName:  "Des Bois",

			BirthDate: pgtype.Date{
				Time:  time.Date(1990, 5, 12, 0, 0, 0, 0, time.UTC),
				Valid: true,
			},

			PhoneNumber: pgtype.Text{
				String: "00 01 02 03 04",
				Valid:  true,
			},

			Email: pgtype.Text{
				String: "robin.desbois@example.com",
				Valid:  true,
			},

			Address: pgtype.Text{
				String: "forêt de Sherwood",
				Valid:  true,
			},
		},
	}

	UpdatePersonFormHandler(cfg, queries)(response, request)

	if response.Code != http.StatusOK {
		t.Errorf(
			"statut obtenu : %d, statut attendu : %d",
			response.Code,
			http.StatusOK,
		)
	}

	if queries.GetPersonByIDReceived != 1 {
		t.Errorf(
			"id reçu : %d, id attendu : %d",
			queries.GetPersonByIDReceived,
			1,
		)
	}

	body := response.Body.String()

	if !strings.Contains(body, "Robin") {
		t.Errorf("la réponse ne contient pas le prénom Robin")
	}

	if !strings.Contains(body, "Des Bois") {
		t.Errorf("la réponse ne contient pas le nom Des Bois")
	}

	if !strings.Contains(body, "1990-05-12") {
		t.Errorf("la réponse ne contient pas la date de naissance")
	}

	if !strings.Contains(body, "robin.desbois@example.com") {
		t.Errorf("la réponse ne contient pas l'email")
	}

	if !strings.Contains(body, "00 01 02 03 04") {
		t.Errorf("la réponse ne contient pas le numéro de téléphone")
	}

	if !strings.Contains(body, "forêt de Sherwood") {
		t.Errorf("la réponse ne contient pas l'adresse")
	}

}
