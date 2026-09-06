package handlers

import (
	"net/http"

	"github.com/grapinou/club-manager/internal/config"
)

func WhereHandler(cfg config.Config) http.HandlerFunc {

	return pageHandler(cfg.SiteName, cfg.Where)

}
