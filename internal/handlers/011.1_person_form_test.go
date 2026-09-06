package handlers

import (
	"testing"

	"github.com/grapinou/club-core/internal/config"
)

func TestPersonFormHandler(t *testing.T) {

	cfg := config.Config{
		SiteName: "Club Manager",
	}

	testHandler(t, "/persons/new", "Ajouter une personne", PersonFormHandler(cfg))
}
