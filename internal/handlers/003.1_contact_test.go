package handlers

import (
	"testing"

	"github.com/grapinou/club-core/internal/config"
)

func TestContactHandler(t *testing.T) {

	contactCfg := config.ContactConfig{
		Title: "Contact de test",
	}

	cfg := config.Config{
		SiteName: "test sur /contact",
		Contact:  contactCfg,
	}

	testHandler(t, "/contact", "Contact de test", ContactHandler(cfg))
}
