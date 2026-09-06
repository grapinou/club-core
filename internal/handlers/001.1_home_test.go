package handlers

import (
	"testing"

	"github.com/grapinou/club-core/internal/config"
)

func TestHomeHandler(t *testing.T) {

	homeCfg := config.PageConfig{
		Title: "Accueil de test",
	}

	cfg := config.Config{
		SiteName: "Club Manager",
		Home:     homeCfg,
	}

	testHandler(t, "/", "Accueil de test", HomeHandler(cfg))
}
