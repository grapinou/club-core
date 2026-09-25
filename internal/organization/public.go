package organization

import (
	"context"
	"errors"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Public DTOs deliberately omit internal IDs, descriptions of technical groups,
// consent instruments and all administrative data.
type PublicLocation struct{ Name, Address string }
type PublicLink struct{ Label, URL string }
type PublicImage struct {
	Src, WebPSrcset, Alt string
	Width, Height        int32
}
type PublicClub struct {
	Name, ShortName, Description, Email, Phone, PhoneLabel, Website          string
	TrialSessionDescription, TrialEquipmentOffer, TrialEquipmentDetailPrompt string
	TrialItemsToBring                                                        []string
	Images                                                                   map[string]PublicImage
	Locations                                                                []PublicLocation
	Links                                                                    []PublicLink
}
type PublicSlot struct {
	Weekday                                                  int16
	Start, End, Activity, Group, Practice, Location, Address string
}
type PublicTimetable struct {
	Season string
	Slots  []PublicSlot
}

var ErrAmbiguousSeason = errors.New("multiple current seasons")

func (s *Service) PublicIdentity(ctx context.Context) (PublicClub, error) {
	identity, err := s.Identity(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicClub{}, nil
	}
	if err != nil {
		return PublicClub{}, err
	}
	o := identity.Organization
	c := PublicClub{Name: o.Name, ShortName: o.ShortName.String, Description: o.Description.String, Email: o.PublicEmail.String, Phone: o.PublicPhone.String, PhoneLabel: o.PublicPhoneLabel.String, Website: o.WebsiteUrl.String,
		TrialSessionDescription: o.TrialSessionDescription.String, TrialItemsToBring: o.TrialItemsToBring, TrialEquipmentOffer: o.TrialEquipmentOffer.String, TrialEquipmentDetailPrompt: o.TrialEquipmentDetailPrompt.String, Images: map[string]PublicImage{}}
	for _, l := range identity.Locations {
		if l.IsActive {
			c.Locations = append(c.Locations, PublicLocation{l.Name, l.Address})
		}
	}
	for _, l := range identity.Links {
		if l.IsActive {
			c.Links = append(c.Links, PublicLink{l.Label, l.Url})
		}
	}
	for _, image := range identity.Images {
		c.Images[image.Placement] = PublicImage{image.Src, image.WebpSrcset, image.Alt, image.Width, image.Height}
	}
	return c, nil
}
func (s *Service) PublicActivities(ctx context.Context) ([]string, error) {
	rows, err := s.queries.AdministrativeActivities(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, r := range rows {
		names = append(names, r.Name)
	}
	return names, nil
}
func (s *Service) PublicMembershipTypes(ctx context.Context) ([]string, error) {
	rows, err := s.queries.AdministrativeMembershipTypes(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, r := range rows {
		names = append(names, r.Name)
	}
	return names, nil
}

// now must be expressed in the application timezone. Re-read every request;
// no fallback to historical/future seasons and no arbitrary overlap selection.
func (s *Service) PublicSchedule(ctx context.Context, now time.Time) (PublicTimetable, error) {
	var result PublicTimetable
	o, err := s.queries.GetActiveOrganization(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	today := pgtype.Date{Time: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
	seasons, err := s.queries.ListCurrentPublicSeasons(ctx, today)
	if err != nil {
		return result, err
	}
	if len(seasons) == 0 {
		return result, nil
	}
	if len(seasons) > 1 {
		return result, ErrAmbiguousSeason
	}
	result.Season = seasons[0].Name
	rows, err := s.queries.ListPublicSchedule(ctx, dbsqlc.ListPublicScheduleParams{Today: today, SeasonID: seasons[0].ID, OrganizationID: o.ID})
	if err != nil {
		return result, err
	}
	for _, r := range rows {
		result.Slots = append(result.Slots, PublicSlot{r.Weekday, r.StartTime, r.EndTime, r.ActivityName, r.GroupName, r.PracticeLabel, r.LocationName, r.LocationAddress})
	}
	return result, nil
}
