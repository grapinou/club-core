package handlers

import (
	"net/http"

	"github.com/grapinou/club-manager/internal/config"
)

func ClubHandler(cfg config.Config) http.HandlerFunc {

	return pageHandler(cfg.SiteName, cfg.Club)

}
