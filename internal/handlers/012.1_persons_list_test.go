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

func TestPersonsListHandler(t *testing.T) {

	cfg := config.Config{
		SiteName: "Club Manager",
	}

	queries := &recordingPersonQueries{
		PersonsList: []dbsqlc.Person{
			{
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
					String: "Bois de Sherwood",
					Valid:  true,
				},

				CreatedAt: pgtype.Timestamptz{
					Time:  time.Date(2009, 10, 10, 15, 4, 0, 0, time.UTC),
					Valid: true,
				},
			},
			{
				ID:        2,
				FirstName: "Bob",
				LastName:  "Sinclair",

				BirthDate: pgtype.Date{
					Time:  time.Date(1925, 7, 10, 0, 0, 0, 0, time.UTC),
					Valid: true,
				},

				PhoneNumber: pgtype.Text{
					String: "   ",
					Valid:  true,
				},

				Email: pgtype.Text{
					Valid: false,
				},

				Address: pgtype.Text{
					String: "Rue du Magnifique",
					Valid:  true,
				},

				CreatedAt: pgtype.Timestamptz{
					Time:  time.Date(2009, 10, 10, 15, 4, 0, 0, time.UTC),
					Valid: true,
				},
			},
		},
	}

	request := httptest.NewRequest(http.MethodGet, "/persons", nil)

	response := httptest.NewRecorder()

	PersonsListHandler(cfg, queries)(response, request)

	if response.Code != http.StatusOK {
		t.Errorf(
			"statut obtenu : %d, statut attendu : %d",
			response.Code,
			http.StatusOK,
		)
	}

	body := response.Body.String()

	if !strings.Contains(body, "Robin") {
		t.Errorf("la réponse ne contient pas le prénom Robin")
	}

	if !strings.Contains(body, "12/05/1990") {
		t.Errorf("la réponse ne contient pas la date de naissance formatée")
	}

	if !strings.Contains(body, "robin.desbois@example.com") {
		t.Errorf("la réponse ne contient pas l'email de Robin")
	}

	if !strings.Contains(body, "Non renseigné") {
		t.Errorf("la réponse ne contient pas la valeur prévue pour un email absent")
	}

	if !strings.Contains(body, "10/10/2009 15:04") {
		t.Errorf("la réponse ne contient pas la date de création formatée")
	}

	if !strings.Contains(body, "00 01 02 03 04") {
		t.Errorf("la réponse ne contient pas le numéro de téléphone de Robin")
	}

	if !strings.Contains(body, "Bois de Sherwood") {
		t.Errorf("la réponse ne contient pas l'adresse de Robin")
	}

}
