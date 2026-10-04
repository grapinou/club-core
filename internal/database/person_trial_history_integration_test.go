package database

import (
	"slices"
	"testing"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

func TestPersonTrialHistoryExplicitDescendingOrder(t *testing.T) {
	db := newTestDatabase(t)
	ctx := t.Context()
	id := func(query string, args ...any) int32 {
		t.Helper()
		var id int32
		if err := db.QueryRowContext(ctx, query, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	person := id("INSERT INTO persons(first_name,last_name) VALUES('History','Person') RETURNING id")
	activity := id("INSERT INTO activities(name) VALUES('JJB') RETURNING id")
	group := id("INSERT INTO groups(activity_id,name) VALUES(?1,'Adultes') RETURNING id", activity)
	season := id("INSERT INTO seasons(name,starts_at,ends_at) VALUES('2026/2027','2026-09-01','2027-08-31') RETURNING id")
	slot := func(start, end string) int32 {
		return id("INSERT INTO group_slots(group_id,season_id,weekday,start_time,end_time,valid_from) VALUES(?1,?2,3,?3,?4,'2026-09-01') RETURNING id", group, season, start, end)
	}
	early, late := slot("10:00", "11:00"), slot("18:00", "19:00")
	create := func(date string, slot any) int32 {
		return id("INSERT INTO trial_registrations(person_id,activity_id,group_id,group_slot_id,trial_date,status) VALUES(?1,?2,?3,?4,?5,'registered') RETURNING id", person, activity, group, slot, date)
	}
	newest := create("2027-01-06", early)
	lateFirst := create("2026-10-07", late)
	earlyTrial := create("2026-10-07", early)
	oldest := create("2026-09-02", late)
	unscheduled := create("2026-10-07", nil)
	lateSecond := create("2026-10-07", late)
	rows, err := dbsqlc.New(db).ListPersonTrials(ctx, person)
	if err != nil {
		t.Fatal(err)
	}
	var got []int32
	for _, row := range rows {
		got = append(got, row.TrialID)
		if row.PersonID != person || !slices.Equal(row.SeasonIds, []int32{season}) {
			t.Fatal("incorrect person or season", row)
		}
	}
	want := []int32{newest, lateSecond, lateFirst, earlyTrial, unscheduled, oldest}
	if !slices.Equal(got, want) {
		t.Fatalf("history order %v want %v", got, want)
	}
}
