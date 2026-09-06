package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPostPersonHandler(t *testing.T) {

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
	request := httptest.NewRequest(http.MethodPost, "/persons", strings.NewReader(form.Encode()))

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response := httptest.NewRecorder()

	queries := &recordingPersonQueries{}

	PostPersonHandler(queries)(response, request)

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

	if !queries.CreatePersonCalled {
		t.Error("CreatePerson aurait dû être appelée")
	}

	if queries.CreatePersonParams.FirstName != expectedFirstName {
		t.Errorf(
			"prénom obtenu : %q, attendu : %q",
			queries.CreatePersonParams.FirstName,
			expectedFirstName,
		)
	}

	if queries.CreatePersonParams.LastName != expectedLastName {
		t.Errorf(
			"nom obtenu : %q, attendu : %q",
			queries.CreatePersonParams.LastName,
			expectedLastName,
		)
	}

	if !queries.CreatePersonParams.BirthDate.Valid {
		t.Error("la date de naissance devrait être valide")
	}

	if queries.CreatePersonParams.BirthDate.Time.Format("2006-01-02") != birthdate {
		t.Errorf(
			"date obtenue : %q, attendue : %q",
			queries.CreatePersonParams.BirthDate.Time.Format("2006-01-02"),
			birthdate,
		)
	}

	if !queries.CreatePersonParams.PhoneNumber.Valid {
		t.Error("le numéro de téléphone devrait être valide")
	}

	if queries.CreatePersonParams.PhoneNumber.String != phoneNumber {
		t.Errorf(
			"numéro de téléphone obtenu : %q, attendu : %q",
			queries.CreatePersonParams.PhoneNumber.String,
			phoneNumber,
		)
	}

	if !queries.CreatePersonParams.Email.Valid {
		t.Error("l'email devrait être valide")
	}

	if queries.CreatePersonParams.Email.String != email {
		t.Errorf(
			"email obtenu : %q, attendu : %q",
			queries.CreatePersonParams.Email.String,
			email,
		)
	}

	if !queries.CreatePersonParams.Address.Valid {
		t.Error("l'addresse devrait être valide")
	}

	if queries.CreatePersonParams.Address.String != address {
		t.Errorf(
			"adresse obtenue : %q, attendu : %q",
			queries.CreatePersonParams.Address.String,
			address,
		)
	}
}

func TestPostPersonHandlerMissingName(t *testing.T) {

	form := url.Values{}

	firstName := "Robin  "
	lastName := " "
	birthdate := "1990-05-12"

	form.Set("FirstName", firstName)
	form.Set("LastName", lastName)
	form.Set("Birthdate", birthdate)

	// attention, la request est en post
	request := httptest.NewRequest(http.MethodPost, "/persons", strings.NewReader(form.Encode()))

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response := httptest.NewRecorder()

	queries := &recordingPersonQueries{}

	PostPersonHandler(queries)(response, request)

	if response.Code != http.StatusBadRequest {
		t.Errorf(
			"statut obtenu : %d, statut attendu : %d",
			response.Code,
			http.StatusBadRequest,
		)
	}

	if queries.CreatePersonCalled {
		t.Error("CreatePerson ne devait pas être appelée")
	}

}

func TestPostPersonHandlerInvalidBirthDate(t *testing.T) {
	// date invalide

	form := url.Values{}

	firstName := "Robin  "
	lastName := "Des Bois "
	birthdate := "01/02/1903"

	form.Set("FirstName", firstName)
	form.Set("LastName", lastName)
	form.Set("Birthdate", birthdate)

	// attention, la request est en post
	request := httptest.NewRequest(http.MethodPost, "/persons", strings.NewReader(form.Encode()))

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response := httptest.NewRecorder()

	queries := &recordingPersonQueries{}

	PostPersonHandler(queries)(response, request)

	if response.Code != http.StatusBadRequest {
		t.Errorf(
			"statut obtenu : %d, statut attendu : %d",
			response.Code,
			http.StatusBadRequest,
		)
	}

	if queries.CreatePersonCalled {
		t.Error("CreatePerson ne devait pas être appelée")
	}
}

func TestPostPersonHandlerDatabaseError(t *testing.T) {
	form := url.Values{}

	firstName := "   Robin    "
	lastName := "Des Bois  "
	birthdate := "1990-05-12"
	phoneNumber := "0001020304"
	email := "robin.desbois@example.com"
	address := "forêt de Sherwood"

	form.Set("FirstName", firstName)
	form.Set("LastName", lastName)
	form.Set("Birthdate", birthdate)
	form.Set("PhoneNumber", phoneNumber)
	form.Set("Email", email)
	form.Set("Address", address)

	// attention, la request est en post
	request := httptest.NewRequest(http.MethodPost, "/persons", strings.NewReader(form.Encode()))

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response := httptest.NewRecorder()

	queries := &recordingPersonQueries{
		CreatePersonError: errors.New("database error"),
	}

	PostPersonHandler(queries)(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Errorf(
			"statut obtenu : %d, statut attendu : %d",
			response.Code,
			http.StatusInternalServerError,
		)
	}

	if !queries.CreatePersonCalled {
		t.Error("CreatePerson aurait dû être appelée")
	}
}
