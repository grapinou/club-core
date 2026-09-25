package database

import (
	"testing"

	"github.com/grapinou/club-core/internal/demodata"
)

func TestBudokanUpgradePreservesExistingTrials(t *testing.T) {
	db := newTestDatabaseNamed(t, "upgrade_budokan_demo")
	ctx := t.Context()
	if err := demodata.SeedBudokan(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("DELETE FROM organization_public_images")
	exec("UPDATE organizations SET public_phone_label=NULL,trial_session_description=NULL,trial_items_to_bring='{}',trial_equipment_offer=NULL,trial_equipment_detail_prompt=NULL")
	exec("DELETE FROM group_slots WHERE weekday=1 AND start_time='20:15'")
	var oldSlot int32
	err := db.QueryRow(ctx, `WITH technical AS (INSERT INTO groups(activity_id,name,show_name_publicly) SELECT id,'JJB pratiques spécifiques',false FROM activities WHERE name='Jiu-Jitsu Brésilien' RETURNING id)
 INSERT INTO group_slots(group_id,season_id,weekday,start_time,end_time,location_id,practice_label,valid_from,valid_until)
 SELECT technical.id,s.id,1,'20:15','22:00',l.id,'Préparation physique / Jiu-Jitsu Brésilien',s.starts_at,s.ends_at FROM technical CROSS JOIN seasons s CROSS JOIN locations l RETURNING id`).Scan(&oldSlot)
	if err != nil {
		t.Fatal(err)
	}
	var person, trial int32
	if err = db.QueryRow(ctx, "INSERT INTO persons(first_name,last_name) VALUES('Personne','Conservée') RETURNING id").Scan(&person); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, "INSERT INTO trial_registrations(person_id,activity_id,group_id,group_slot_id,trial_date,status) SELECT $1,g.activity_id,g.id,$2,'2026-09-28','registered' FROM groups g JOIN group_slots gs ON gs.group_id=g.id WHERE gs.id=$2 RETURNING id", person, oldSlot).Scan(&trial); err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		if err = demodata.UpgradeBudokan(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var people, trials, images, activeMonday, oldActive int
	err = db.QueryRow(ctx, `SELECT (SELECT count(*) FROM persons),(SELECT count(*) FROM trial_registrations),(SELECT count(*) FROM organization_public_images),(SELECT count(*) FROM group_slots WHERE weekday=1 AND start_time='20:15' AND is_active),(SELECT count(*) FROM group_slots WHERE id=$1 AND is_active)`, oldSlot).Scan(&people, &trials, &images, &activeMonday, &oldActive)
	if err != nil || people != 1 || trials != 1 || images != 5 || activeMonday != 1 || oldActive != 0 {
		t.Fatalf("upgrade drift: %d %d %d %d %d %v", people, trials, images, activeMonday, oldActive, err)
	}
	var retainedSlot int32
	if err = db.QueryRow(ctx, "SELECT group_slot_id FROM trial_registrations WHERE id=$1", trial).Scan(&retainedSlot); err != nil || retainedSlot != oldSlot {
		t.Fatal("historical trial changed", retainedSlot, err)
	}
}
