package minorsafety

import (
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5/pgtype"
	"testing"
	"time"
)

func TestClubCoreSafety(t *testing.T) {
	at := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	person := func(id int32, birth string) dbsqlc.ListInteractionParticipantsRow {
		date, _ := time.Parse("2006-01-02", birth)
		return dbsqlc.ListInteractionParticipantsRow{PersonID: id, BirthDate: pgtype.Date{Time: date, Valid: true}}
	}
	adult := person(1, "1980-01-01")
	child := person(2, "2011-09-12")
	guardian := person(3, "1980-01-01")
	edge := dbsqlc.ListInteractionGuardianEdgesRow{ChildPersonID: 2, GuardianPersonID: 3}
	for _, tc := range []struct {
		name       string
		people     []dbsqlc.ListInteractionParticipantsRow
		edges      []dbsqlc.ListInteractionGuardianEdgesRow
		want       Decision
		supervised bool
	}{
		{"fourteen", []dbsqlc.ListInteractionParticipantsRow{adult, person(2, "2012-09-12")}, nil, SupervisionRequired, false},
		{"adult to fifteen", []dbsqlc.ListInteractionParticipantsRow{adult, child}, nil, SupervisionRequired, false},
		{"fifteen to adult", []dbsqlc.ListInteractionParticipantsRow{child, adult}, nil, SupervisionRequired, false},
		{"seventeen", []dbsqlc.ListInteractionParticipantsRow{adult, person(2, "2009-09-12")}, nil, SupervisionRequired, false},
		{"eighteen", []dbsqlc.ListInteractionParticipantsRow{adult, person(2, "2008-09-12")}, nil, NotApplicable, false},
		{"guardian", []dbsqlc.ListInteractionParticipantsRow{guardian, child}, []dbsqlc.ListInteractionGuardianEdgesRow{edge}, Allowed, false},
		{"reverse guardian", []dbsqlc.ListInteractionParticipantsRow{child, guardian}, []dbsqlc.ListInteractionGuardianEdgesRow{edge}, Allowed, false},
		{"false guardian", []dbsqlc.ListInteractionParticipantsRow{adult, child}, []dbsqlc.ListInteractionGuardianEdgesRow{edge}, SupervisionRequired, false},
		{"supervised", []dbsqlc.ListInteractionParticipantsRow{adult, child, guardian}, []dbsqlc.ListInteractionGuardianEdgesRow{edge}, Allowed, true},
		{"multiple adults insufficient", []dbsqlc.ListInteractionParticipantsRow{adult, child, guardian}, nil, SupervisionRequired, false},
		{"every child needs supervision", []dbsqlc.ListInteractionParticipantsRow{adult, child, guardian, person(4, "2010-01-01")}, []dbsqlc.ListInteractionGuardianEdgesRow{edge}, SupervisionRequired, false},
		{"minor minor", []dbsqlc.ListInteractionParticipantsRow{child, person(4, "2010-01-01")}, nil, NotApplicable, false},
		{"adult adult", []dbsqlc.ListInteractionParticipantsRow{adult, guardian}, nil, NotApplicable, false},
		{"unknown birth", []dbsqlc.ListInteractionParticipantsRow{adult, {PersonID: 2}}, nil, Denied, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluate(tc.people, tc.edges, at)
			if got.Decision != tc.want || got.Supervised != tc.supervised || got.Basis != RuleClubCoreSafety {
				t.Fatalf("%+v", got)
			}
		})
	}
}
