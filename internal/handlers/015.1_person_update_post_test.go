package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPostUpdatePersonHandler(t *testing.T) {

	form := url.Values{}

	firstName := "   Robin    "
	expectedFirstName := "Robin"
	lastName := "Des Bois  "
	expectedLastName := "Des Bois"
	birthdate := "1990-05-12"
	phoneNumber := "00 01 02 03 04"
	email := "robin.desbois@example.com"
	address := "forêt de Sherwood"

	form.Set("FirstName", firstName)
	form.Set("LastName", lastName)
	form.Set("Birthdate", birthdate)
	form.Set("PhoneNumber", phoneNumber)
	form.Set("Email", email)
	form.Set("Address", address)

	// attention, la request est en post
	request := httptest.NewRequest(http.MethodPost, "/persons/42/edit", strings.NewReader(form.Encode()))

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	request.SetPathValue("id", "42")

	response := httptest.NewRecorder()

	queries := &recordingPersonQueries{}

	PostUpdatePersonHandler(queries)(response, request)

	if response.Code != http.StatusSeeOther {
		t.Errorf(
			"statut obtenu : %d, statut attendu : %d",
			response.Code,
			http.StatusSeeOther,
		)
	}

	location := response.Header().Get("Location")

	if location != "/persons" {
		t.Errorf(
			"redirection obtenue : %q, attendue : %q",
			location,
			"/persons",
		)
	}

	if !queries.UpdatePersonCalled {
		t.Fatal("UpdatePerson n'a pas été appelé")
	}

	if queries.UpdatePersonParams.ID != 42 {
		t.Errorf(
			"id obtenu : %d, attendu : %d",
			queries.UpdatePersonParams.ID,
			42,
		)
	}
	if queries.UpdatePersonParams.FirstName != expectedFirstName {
		t.Errorf(
			"prénom obtenu : %q, attendu : %q",
			queries.UpdatePersonParams.FirstName,
			expectedFirstName,
		)
	}

	if queries.UpdatePersonParams.LastName != expectedLastName {
		t.Errorf(
			"nom obtenu : %q, attendu : %q",
			queries.UpdatePersonParams.LastName,
			expectedLastName,
		)
	}

	if !queries.UpdatePersonParams.BirthDate.Valid {
		t.Error("la date de naissance devrait être valide")
	}

	if queries.UpdatePersonParams.BirthDate.Time.Format("2006-01-02") != birthdate {
		t.Errorf(
			"date obtenue : %q, attendue : %q",
			queries.UpdatePersonParams.BirthDate.Time.Format("2006-01-02"),
			birthdate,
		)
	}

	if !queries.UpdatePersonParams.PhoneNumber.Valid {
		t.Error("le numéro de téléphone devrait être valide")
	}

	if queries.UpdatePersonParams.PhoneNumber.String != phoneNumber {
		t.Errorf(
			"numéro de téléphone obtenu : %q, attendu : %q",
			queries.UpdatePersonParams.PhoneNumber.String,
			phoneNumber,
		)
	}

	if !queries.UpdatePersonParams.Email.Valid {
		t.Error("l'email devrait être valide")
	}

	if queries.UpdatePersonParams.Email.String != email {
		t.Errorf(
			"email obtenu : %q, attendu : %q",
			queries.UpdatePersonParams.Email.String,
			email,
		)
	}

	if !queries.UpdatePersonParams.Address.Valid {
		t.Error("l'addresse devrait être valide")
	}

	if queries.UpdatePersonParams.Address.String != address {
		t.Errorf(
			"adresse obtenue : %q, attendu : %q",
			queries.UpdatePersonParams.Address.String,
			address,
		)
	}
}
