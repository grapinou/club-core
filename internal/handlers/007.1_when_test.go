package handlers

import (
	"testing"

	"github.com/grapinou/club-core/internal/config"
)

func TestWhenHandler(t *testing.T) {

	whenCfg := config.PageConfig{
		Title: "Quand de test",
	}

	cfg := config.Config{
		SiteName: "Club Manager",
		When:     whenCfg,
	}

	testHandler(t, "/when", "Quand de test", WhenHandler(cfg))
}
