// Package organization exposes the club's database identity and reference data.
// It does not impose tenant ownership on the historical membership domains.
package organization

import (
	"context"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	queries *dbsqlc.Queries
	db      *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Service { return &Service{queries: dbsqlc.New(db), db: db} }

// Identity is read afresh; no club-specific runtime configuration or cache.
type Identity struct {
	Organization dbsqlc.Organization
	Locations    []dbsqlc.Location
	Links        []dbsqlc.OrganizationLink
	Images       []dbsqlc.OrganizationPublicImage
}

func (s *Service) Identity(ctx context.Context) (Identity, error) {
	var result Identity
	var err error
	result.Organization, err = s.queries.GetActiveOrganization(ctx)
	if err != nil {
		return result, err
	}
	result.Locations, err = s.queries.ListOrganizationLocations(ctx, result.Organization.ID)
	if err != nil {
		return result, err
	}
	result.Links, err = s.queries.ListOrganizationLinks(ctx, result.Organization.ID)
	if err != nil {
		return result, err
	}
	result.Images, err = s.queries.ListOrganizationPublicImages(ctx, result.Organization.ID)
	return result, err
}

// Schedule returns the active published schedule for an explicit season, not
// just sessions valid today. Validity dates remain available to future callers.
func (s *Service) Schedule(ctx context.Context, seasonID int32) ([]dbsqlc.ListReferenceScheduleRow, error) {
	return s.queries.ListReferenceSchedule(ctx, seasonID)
}

// Catalogue is an operator/future frontend projection built exclusively from DB
// references. An empty membership type list is meaningful, not a fallback tariff.
type Catalogue struct {
	Identity        Identity
	Season          dbsqlc.Season
	Activities      []dbsqlc.AdministrativeActivitiesRow
	Groups          []dbsqlc.ListActiveGroupsRow
	Schedule        []dbsqlc.ListReferenceScheduleRow
	MembershipTypes []dbsqlc.AdministrativeMembershipTypesRow
	Consents        []dbsqlc.ConsentDefinition
}

func (s *Service) Catalogue(ctx context.Context, seasonName string) (Catalogue, error) {
	var c Catalogue
	var err error
	if c.Identity, err = s.Identity(ctx); err != nil {
		return c, err
	}
	if c.Season, err = s.queries.GetReferenceSeason(ctx, seasonName); err != nil {
		return c, err
	}
	if c.Activities, err = s.queries.AdministrativeActivities(ctx); err != nil {
		return c, err
	}
	if c.Groups, err = s.queries.ListActiveGroups(ctx); err != nil {
		return c, err
	}
	if c.Schedule, err = s.Schedule(ctx, c.Season.ID); err != nil {
		return c, err
	}
	if c.MembershipTypes, err = s.queries.AdministrativeMembershipTypes(ctx); err != nil {
		return c, err
	}
	c.Consents, err = s.queries.ListActiveConsentDefinitions(ctx)
	return c, err
}
