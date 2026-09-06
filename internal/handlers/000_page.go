package handlers

import (
	"net/http"

	"github.com/grapinou/club-manager/internal/config"
	"github.com/grapinou/club-manager/internal/views"
)

func pageHandler(siteName string, page config.PageConfig) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		data := views.PageData{
			SiteName:    siteName,
			Title:       page.Title + " - " + siteName,
			Heading:     page.Heading,
			Description: page.Description,
			Image:       page.Image,
			ImageAlt:    page.ImageAlt,
		}

		err := views.RenderPage(w, data)

		if err != nil {
			http.Error(
				w,
				"Erreur interne du serveur",
				http.StatusInternalServerError,
			)
		}
	}
}
