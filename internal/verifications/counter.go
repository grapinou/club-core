// Package verifications composes the existing intervention counts for navigation.
package verifications

import "context"

type registrationCounter interface {
	CountOpen(context.Context, int32) (int64, error)
}
type correctionCounter interface {
	CountPendingIdentityCorrections(context.Context) (int64, error)
}
type Counter struct {
	registrations registrationCounter
	corrections   correctionCounter
}

func New(registrations registrationCounter, corrections correctionCounter) *Counter {
	return &Counter{registrations, corrections}
}
func (c *Counter) CountOpen(ctx context.Context, actor int32) (int64, error) {
	// CountOpen checks the activated actor and registrations.review before any
	// correction count is read. Access also checks this permission before calling.
	registrations, err := c.registrations.CountOpen(ctx, actor)
	if err != nil {
		return 0, err
	}
	corrections, err := c.corrections.CountPendingIdentityCorrections(ctx)
	if err != nil {
		return 0, err
	}
	return registrations + corrections, nil
}
