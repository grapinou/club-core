package verifications

import (
	"context"
	"errors"
	"testing"
)

type registrationStub struct{ err error }

func (s registrationStub) CountOpen(context.Context, int32) (int64, error) { return 2, s.err }

type correctionStub struct{ calls int }

func (s *correctionStub) CountPendingIdentityCorrections(context.Context) (int64, error) {
	s.calls++
	return 1, nil
}
func TestCounterDoesNotReadCorrectionsWhenRegistrationReviewDenied(t *testing.T) {
	denied := errors.New("permission denied")
	corrections := &correctionStub{}
	if _, err := New(registrationStub{denied}, corrections).CountOpen(t.Context(), 1); !errors.Is(err, denied) || corrections.calls != 0 {
		t.Fatal("correction count read without review authorization")
	}
	if count, err := New(registrationStub{}, corrections).CountOpen(t.Context(), 1); err != nil || count != 3 {
		t.Fatal(count, err)
	}
}
