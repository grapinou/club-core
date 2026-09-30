package database

import (
	"database/sql"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/database/dbtypes"
)

func TestGroupsIntegration(t *testing.T) {
	db := newTestDatabase(t)
	ctx := t.Context()
	q := dbsqlc.New(db)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	id := func(sql string, args ...any) int32 {
		t.Helper()
		var n int32
		must(db.QueryRowContext(ctx, sql, args...).Scan(&n))
		return n
	}
	activity := id("INSERT INTO activities (name) VALUES ('JJB') RETURNING id")
	activity2 := id("INSERT INTO activities (name) VALUES ('Judo') RETURNING id")
	season := id("INSERT INTO seasons (name, starts_at, ends_at) VALUES ('Current',date('now','-100 days'),date('now','+100 days')) RETURNING id")
	season2 := id("INSERT INTO seasons (name, starts_at, ends_at) VALUES ('Other',date('now','+101 days'),date('now','+300 days')) RETURNING id")
	membershipType := id("INSERT INTO membership_types (name) VALUES ('Standard') RETURNING id")
	var today dbtypes.Date
	must(db.QueryRowContext(ctx, "SELECT CURRENT_DATE").Scan(&today))
	date := func(days int) dbtypes.Date { return dbtypes.Date{Time: today.Time.AddDate(0, 0, days), Valid: true} }
	person, err := q.CreatePerson(ctx, dbsqlc.CreatePersonParams{FirstName: "Arthur", LastName: "Test"})
	must(err)
	membership := id("INSERT INTO memberships (person_id,season_id,membership_type_id,status) VALUES (?1,?2,?3,'active') RETURNING id", person.ID, season, membershipType)
	otherMembership := id("INSERT INTO memberships (person_id,season_id,membership_type_id,status) VALUES (?1,?2,?3,'active') RETURNING id", person.ID, season2, membershipType)
	group, err := q.CreateGroup(ctx, dbsqlc.CreateGroupParams{ActivityID: activity, Name: "Enfants", IsActive: true})
	must(err)
	group2, err := q.CreateGroup(ctx, dbsqlc.CreateGroupParams{ActivityID: activity, Name: "Compétition", IsActive: true})
	must(err)

	t.Run("groups creation constraints and rich reads", func(t *testing.T) {
		_, err := q.CreateGroup(ctx, dbsqlc.CreateGroupParams{ActivityID: activity, Name: "Enfants", IsActive: true})
		requireSQLiteConstraint(t, err, "2067")
		_, err = q.CreateGroup(ctx, dbsqlc.CreateGroupParams{ActivityID: activity2, Name: "Enfants", IsActive: true})
		must(err)
		_, err = q.CreateGroup(ctx, dbsqlc.CreateGroupParams{ActivityID: -1, Name: "Missing"})
		requireSQLiteConstraint(t, err, "787")
		got, err := q.GetGroup(ctx, group.ID)
		must(err)
		if got.ActivityID != activity || got.ActivityName != "JJB" || got.Name != "Enfants" || got.Description.Valid {
			t.Fatalf("group summary: %+v", got)
		}
		all, err := q.ListGroups(ctx)
		must(err)
		if len(all) != 3 {
			t.Fatalf("groups: %+v", all)
		}
		// Make timestamp maintenance observable without sleeps.
		_, err = db.ExecContext(ctx, "UPDATE groups SET updated_at = strftime('%Y-%m-%d %H:%M:%f',created_at,'-1 day') WHERE id=?1", group.ID)
		must(err)
		updated, err := q.UpdateGroup(ctx, dbsqlc.UpdateGroupParams{ID: group.ID, Name: "Enfants 1", Description: sql.NullString{String: "Description", Valid: true}, IsActive: false})
		must(err)
		if updated.Name != "Enfants 1" || !updated.Description.Valid || updated.IsActive || updated.UpdatedAt.Time.Before(updated.CreatedAt.Time) {
			t.Fatalf("update: %+v", updated)
		}
		active, err := q.ListActiveGroups(ctx)
		must(err)
		if len(active) != 2 {
			t.Fatalf("active groups: %+v", active)
		}
		got, err = q.GetGroup(ctx, group.ID)
		must(err)
		if got.IsActive {
			t.Fatal("deactivated group should remain readable")
		}
	})

	t.Run("membership assignment history and current members", func(t *testing.T) {
		assignment, err := q.AssignMembershipGroup(ctx, dbsqlc.AssignMembershipGroupParams{MembershipID: membership, GroupID: group.ID, JoinedAt: date(-30)})
		must(err)
		_, err = q.AssignMembershipGroup(ctx, dbsqlc.AssignMembershipGroupParams{MembershipID: membership, GroupID: group.ID, JoinedAt: date(-20)})
		requireSQLiteConstraint(t, err, "2067")
		_, err = q.CloseMembershipGroup(ctx, dbsqlc.CloseMembershipGroupParams{ID: assignment.ID, LeftAt: date(-31)})
		requireSQLiteConstraint(t, err, "275")
		current, err := q.ListCurrentMembershipGroups(ctx, membership)
		must(err)
		if len(current) != 1 || current[0].GroupID != group.ID || current[0].ActivityName != "JJB" {
			t.Fatalf("current: %+v", current)
		}
		closed, err := q.CloseMembershipGroup(ctx, dbsqlc.CloseMembershipGroupParams{ID: assignment.ID, LeftAt: today})
		must(err)
		if !closed.LeftAt.Valid {
			t.Fatal("missing closure date")
		}
		current, err = q.ListCurrentMembershipGroups(ctx, membership)
		must(err)
		if len(current) != 0 {
			t.Fatalf("closed assignment still current: %+v", current)
		}
		_, err = q.AssignMembershipGroup(ctx, dbsqlc.AssignMembershipGroupParams{MembershipID: membership, GroupID: group2.ID, JoinedAt: today})
		must(err)
		// Rejoining the first group is allowed and preserves the old record.
		_, err = q.AssignMembershipGroup(ctx, dbsqlc.AssignMembershipGroupParams{MembershipID: membership, GroupID: group.ID, JoinedAt: today})
		must(err)
		current, err = q.ListCurrentMembershipGroups(ctx, membership)
		must(err)
		if len(current) != 2 {
			t.Fatalf("simultaneous groups: %+v", current)
		}
		history, err := q.ListMembershipGroupHistory(ctx, membership)
		must(err)
		if len(history) != 3 || history[0].ID != assignment.ID || !history[0].LeftAt.Valid {
			t.Fatalf("history: %+v", history)
		}
		_, err = q.AssignMembershipGroup(ctx, dbsqlc.AssignMembershipGroupParams{MembershipID: otherMembership, GroupID: group2.ID, JoinedAt: today})
		must(err)
		members, err := q.ListCurrentGroupMembers(ctx, dbsqlc.ListCurrentGroupMembersParams{GroupID: group2.ID, SeasonID: season})
		must(err)
		if len(members) != 1 || members[0].MembershipID != membership || members[0].PersonID != person.ID || members[0].FirstName != "Arthur" || members[0].LastName != "Test" || members[0].BirthDate.Valid || !members[0].JoinedAt.Time.Equal(today.Time) {
			t.Fatalf("rich members: %+v", members)
		}
		for _, tc := range []struct {
			status  string
			present bool
		}{
			{"pending", true},
			{"active", true},
			{"ended", false},
			{"cancelled", false},
		} {
			t.Run(tc.status, func(t *testing.T) {
				if _, err := db.ExecContext(ctx, "UPDATE memberships SET status=?2 WHERE id=?1", membership, tc.status); err != nil {
					t.Fatal(err)
				}
				members, err := q.ListCurrentGroupMembers(ctx, dbsqlc.ListCurrentGroupMembersParams{GroupID: group2.ID, SeasonID: season})
				if err != nil {
					t.Fatal(err)
				}
				if tc.present {
					if len(members) != 1 || members[0].MembershipID != membership {
						t.Fatalf("%s membership should be present: %+v", tc.status, members)
					}
				} else if len(members) != 0 {
					t.Fatalf("%s membership should be absent: %+v", tc.status, members)
				}
				history, err := q.ListMembershipGroupHistory(ctx, membership)
				if err != nil {
					t.Fatal(err)
				}
				if len(history) != 3 || history[0].ID != assignment.ID || !history[0].LeftAt.Valid || !history[0].LeftAt.Time.Equal(today.Time) {
					t.Fatalf("%s membership history must remain accessible: %+v", tc.status, history)
				}
			})
		}
		future, err := q.AssignMembershipGroup(ctx, dbsqlc.AssignMembershipGroupParams{MembershipID: otherMembership, GroupID: group.ID, JoinedAt: date(1)})
		must(err)
		members, err = q.ListCurrentGroupMembers(ctx, dbsqlc.ListCurrentGroupMembersParams{GroupID: group.ID, SeasonID: season2})
		must(err)
		if len(members) != 0 {
			t.Fatal("future assignment listed")
		}
		_, err = q.CloseMembershipGroup(ctx, dbsqlc.CloseMembershipGroupParams{ID: future.ID, LeftAt: date(1)})
		must(err)
		_, err = db.ExecContext(ctx, "DELETE FROM groups WHERE id=?1", group.ID)
		requireSQLiteConstraint(t, err, "787")
		_, err = q.AssignMembershipGroup(ctx, dbsqlc.AssignMembershipGroupParams{MembershipID: -1, GroupID: group.ID, JoinedAt: today})
		requireSQLiteConstraint(t, err, "787")
		_, err = q.AssignMembershipGroup(ctx, dbsqlc.AssignMembershipGroupParams{MembershipID: membership, GroupID: -1, JoinedAt: today})
		requireSQLiteConstraint(t, err, "787")
	})

	t.Run("weekly slots history validity and constraints", func(t *testing.T) {
		at := func(hour int) dbtypes.Time {
			return dbtypes.Time{Microseconds: int64(hour) * int64(time.Hour/time.Microsecond), Valid: true}
		}
		params := dbsqlc.CreateGroupSlotParams{GroupID: group.ID, SeasonID: season, Weekday: 2, StartTime: at(18), EndTime: at(19), ValidFrom: date(-30), IsActive: true}
		old, err := q.CreateGroupSlot(ctx, params)
		must(err)
		if old.Location.Valid {
			t.Fatal("location must allow NULL")
		}
		params.Weekday = 4
		second, err := q.CreateGroupSlot(ctx, params)
		must(err)
		closed, err := q.CloseGroupSlot(ctx, dbsqlc.CloseGroupSlotParams{ID: old.ID, ValidUntil: date(-1)})
		must(err)
		if !closed.ValidUntil.Valid {
			t.Fatal("slot not closed")
		}
		params.Weekday = 3
		params.StartTime = at(17)
		params.EndTime = at(18)
		params.ValidFrom = today
		params.Location = sql.NullString{String: "Dojo municipal", Valid: true}
		replacement, err := q.CreateGroupSlot(ctx, params)
		must(err)
		currentParams := dbsqlc.ListCurrentGroupSlotsParams{GroupID: group.ID, SeasonID: season}
		current, err := q.ListCurrentGroupSlots(ctx, currentParams)
		must(err)
		if len(current) != 2 {
			t.Fatalf("current slots: %+v", current)
		}
		for _, slot := range current {
			if slot.ID == old.ID || slot.GroupName != "Enfants 1" || slot.SeasonName != "Current" {
				t.Fatalf("rich slot: %+v", slot)
			}
		}
		params.SeasonID = season2
		_, err = q.CreateGroupSlot(ctx, params)
		must(err)
		all, err := q.ListGroupSlots(ctx, group.ID)
		must(err)
		if len(all) != 4 {
			t.Fatalf("slot history: %+v", all)
		}
		forSeason, err := q.ListGroupSlotsForSeason(ctx, dbsqlc.ListGroupSlotsForSeasonParams{GroupID: group.ID, SeasonID: season})
		must(err)
		if len(forSeason) != 3 {
			t.Fatalf("season slots: %+v", forSeason)
		}
		_, err = db.ExecContext(ctx, "UPDATE group_slots SET updated_at=strftime('%Y-%m-%d %H:%M:%f',created_at,'-1 day') WHERE id=?1", replacement.ID)
		must(err)
		updated, err := q.UpdateGroupSlot(ctx, dbsqlc.UpdateGroupSlotParams{ID: replacement.ID, Weekday: 3, StartTime: at(17), EndTime: at(19), Location: params.Location, ValidFrom: today, ValidUntil: today, IsActive: true})
		must(err)
		if updated.EndTime != at(19) || updated.Location != params.Location || updated.UpdatedAt.Time.Before(updated.CreatedAt.Time) {
			t.Fatalf("updated slot: %+v", updated)
		}
		current, err = q.ListCurrentGroupSlots(ctx, currentParams)
		must(err)
		if len(current) != 2 {
			t.Fatal("valid_until must include today")
		}
		deactivated, err := q.DeactivateGroupSlot(ctx, second.ID)
		must(err)
		if deactivated.IsActive {
			t.Fatal("slot still active")
		}
		params.SeasonID = season
		params.ValidFrom = date(1)
		_, err = q.CreateGroupSlot(ctx, params)
		must(err)
		current, err = q.ListCurrentGroupSlots(ctx, currentParams)
		must(err)
		if len(current) != 1 || current[0].ID != replacement.ID {
			t.Fatalf("inactive/future slots: %+v", current)
		}
		for _, tc := range []struct {
			name   string
			change func(*dbsqlc.CreateGroupSlotParams)
			code   string
		}{
			{"equal times", func(p *dbsqlc.CreateGroupSlotParams) { p.EndTime = p.StartTime }, "275"},
			{"reversed times", func(p *dbsqlc.CreateGroupSlotParams) { p.EndTime = at(1) }, "275"},
			{"reversed dates", func(p *dbsqlc.CreateGroupSlotParams) { p.ValidUntil = date(-1) }, "275"},
			{"weekday zero", func(p *dbsqlc.CreateGroupSlotParams) { p.Weekday = 0 }, "275"},
			{"weekday eight", func(p *dbsqlc.CreateGroupSlotParams) { p.Weekday = 8 }, "275"},
			{"missing group", func(p *dbsqlc.CreateGroupSlotParams) { p.GroupID = -1 }, "787"},
			{"missing season", func(p *dbsqlc.CreateGroupSlotParams) { p.SeasonID = -1 }, "787"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := params
				tc.change(&p)
				_, err := q.CreateGroupSlot(ctx, p)
				requireSQLiteConstraint(t, err, tc.code)
			})
		}
		_, err = q.CloseGroupSlot(ctx, dbsqlc.CloseGroupSlotParams{ID: replacement.ID, ValidUntil: date(-1)})
		requireSQLiteConstraint(t, err, "275")
	})
}
