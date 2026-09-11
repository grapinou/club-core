package router

import (
	"context"
	"net/http"

	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/handlers"
	"github.com/grapinou/club-core/internal/websecurity"
)

func (f *FakeQueries) ListUserRoles(context.Context, int32) ([]dbsqlc.Role, error) { return nil, nil }
func newTestRouter(cfg config.Config, q *FakeQueries) *http.ServeMux {
	return New(cfg, q, handlers.NewAccess(cfg.SiteName, authorization.New(q)), websecurity.NewCSRF(false))
}
