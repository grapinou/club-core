package handlers

import (
	"testing"

	"github.com/grapinou/club-manager/internal/config"
)

func TestClubHandler(t *testing.T) {

	// pour vérifier que la transmission de données provient de ce cfg
	// on le remplace par un autre nom

	clubCfg := config.PageConfig{
		Title: "Le Club de test",
	}

	cfg := config.Config{
		SiteName: "test sur /club",
		Club:     clubCfg,
	}

	testHandler(t, "/club", "Le Club de test", ClubHandler(cfg))
}
