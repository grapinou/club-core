package handlers

import (
	"testing"

	"github.com/grapinou/club-core/internal/config"
)

func TestWhereHandler(t *testing.T) {

	whereCfg := config.PageConfig{
		Title: "Où de test",
	}

	cfg := config.Config{
		SiteName: "Club Manager",
		Where:    whereCfg,
	}

	testHandler(t, "/where", "Où de test", WhereHandler(cfg))
}
