package database

import (
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/demodata"
	"github.com/grapinou/club-core/internal/organization"
	"github.com/jackc/pgx/v5"
)

func TestBudokanSeedPostgres(t *testing.T) {
	db := newTestDatabaseNamed(t, "club_core_demo")
	ctx := t.Context()
	if err := demodata.SeedBudokan(ctx, db, false); !errors.Is(err, demodata.ErrGuard) {
		t.Fatalf("guard: %v", err)
	}
	// Late failure must roll back identity, references, schedule and consent alike.
	_, err := db.Exec(ctx, `CREATE FUNCTION reject_demo_consent() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END $$;
 CREATE TRIGGER reject_demo BEFORE INSERT ON consent_definitions FOR EACH ROW EXECUTE FUNCTION reject_demo_consent();`)
	if err != nil {
		t.Fatal(err)
	}
	if err = demodata.SeedBudokan(ctx, db, true); err == nil {
		t.Fatal("expected injected failure")
	}
	for _, table := range []string{"organizations", "locations", "organization_links", "activities", "groups", "seasons", "group_slots", "membership_types", "consent_definitions"} {
		var n int
		if err = db.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("rollback %s: %d %v", table, n, err)
		}
	}
	if _, err = db.Exec(ctx, "DROP TRIGGER reject_demo ON consent_definitions; DROP FUNCTION reject_demo_consent()"); err != nil {
		t.Fatal(err)
	}
	// Run the actual executable against the isolated, migrated PostgreSQL.
	run := func(flag string) (string, error) {
		cmd := exec.CommandContext(ctx, "go", "run", "../../cmd/clubctl", "seed-budokan", flag)
		cmd.Env = append(os.Environ(), "DATABASE_URL="+db.Config().ConnString())
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := run("--confirm-empty-demo"); err != nil {
		t.Fatalf("CLI: %s %v", out, err)
	}
	inspect := exec.CommandContext(ctx, "go", "run", "../../cmd/clubctl", "describe-club", "2026/2027")
	inspect.Env = append(os.Environ(), "DATABASE_URL="+db.Config().ConnString())
	if out, err := inspect.CombinedOutput(); err != nil || !strings.Contains(string(out), "Budokan Sud Oise") || !strings.Contains(string(out), "JJB No-Gi") {
		t.Fatalf("describe CLI: %s %v", out, err)
	}
	service := organization.New(db)
	identity, err := service.Identity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Organization.Name != "Budokan Sud Oise" || identity.Organization.PublicEmail.String != "budokansud.oise@gmail.com" || identity.Organization.PublicPhone.String != "06 21 03 21 61" || len(identity.Locations) != 1 || len(identity.Links) != 1 {
		t.Fatalf("identity: %+v", identity)
	}
	if identity.Locations[0].Name != "Gymnase La Mardelle" || identity.Locations[0].Address != "Rue des Marais, 60260 Lamorlaye" {
		t.Fatal(identity.Locations)
	}

	catalogue, err := service.Catalogue(ctx, "2026/2027")
	if err != nil {
		t.Fatal(err)
	}
	var activityNames, typeNames []string
	for _, a := range catalogue.Activities {
		activityNames = append(activityNames, a.Name)
	}
	for _, m := range catalogue.MembershipTypes {
		typeNames = append(typeNames, m.Name)
	}
	if !reflect.DeepEqual(activityNames, []string{"Jiu-Jitsu Brésilien", "Jiu-Jitsu Traditionnel / Combat", "Préparation physique"}) {
		t.Fatal(activityNames)
	}
	if !reflect.DeepEqual(typeNames, []string{"Adolescent", "Adulte", "Enfant"}) {
		t.Fatal(typeNames)
	}
	if catalogue.Season.StartsAt.Time.Format("2006-01-02") != "2026-09-01" || catalogue.Season.EndsAt.Time.Format("2006-01-02") != "2027-08-31" {
		t.Fatal(catalogue.Season)
	}
	if len(catalogue.Consents) != 1 || catalogue.Consents[0].Code != "image_rights" || catalogue.Consents[0].Version != 1 {
		t.Fatal(catalogue.Consents)
	}
	if identity.Links[0].Url != "https://www.instagram.com/budokan_sud_oise/" {
		t.Fatal(identity.Links)
	}
	q := dbsqlc.New(db)
	seasons, err := q.AdministrativeSeasons(ctx)
	if err != nil || len(seasons) != 1 || seasons[0].Name != "2026/2027" {
		t.Fatalf("seasons: %v %v", seasons, err)
	}
	slots, err := service.Schedule(ctx, seasons[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{
		"1 18:15 19:15 Jiu-Jitsu Traditionnel / Combat enfants 7–10 ans",
		"1 19:15 20:15 Jiu-Jitsu Traditionnel / Combat enfants 10–14 ans",
		"1 20:15 22:00 Préparation physique / Jiu-Jitsu Brésilien",
		"2 18:15 19:15 JJB enfants 7–10 ans", "2 19:15 20:15 JJB enfants 10–14 ans", "2 20:15 22:00 JJB Adolescents et Adultes",
		"3 18:15 19:15 JJB enfants 7–10 ans", "3 19:15 20:15 JJB enfants 10–14 ans", "3 20:15 22:00 JJB Adolescents et Adultes",
		"4 18:15 19:15 Jiu-Jitsu Traditionnel / Combat enfants 7–10 ans", "4 19:15 20:15 Jiu-Jitsu Traditionnel / Combat enfants 10–14 ans", "4 20:15 22:00 JJB Adolescents et Adultes",
		"5 20:15 22:00 Préparation physique / JJB — Adolescents et Adultes", "6 10:00 12:00 JJB No-Gi", "6 16:00 18:00 JJB Adolescents et Adultes", "7 16:00 18:00 JJB libre",
	}
	var actual []string
	counts := make([]int, 7)
	for _, slot := range slots {
		label := slot.GroupName
		if slot.PracticeLabel.Valid {
			label = slot.PracticeLabel.String
		}
		actual = append(actual, string(rune('0'+slot.Weekday))+" "+slot.StartTime+" "+slot.EndTime+" "+label)
		counts[slot.Weekday-1]++
		if slot.LocationID.Int32 != identity.Locations[0].ID || slot.LocationName != "Gymnase La Mardelle" || slot.LocationAddress != identity.Locations[0].Address {
			t.Fatalf("location: %+v", slot)
		}
	}
	if !reflect.DeepEqual(expected, actual) || !reflect.DeepEqual(counts, []int{3, 3, 3, 3, 1, 2, 1}) {
		t.Fatalf("schedule drift: %v / %v", actual, counts)
	}
	for table, want := range map[string]int{"activities": 3, "groups": 6, "membership_types": 3, "consent_definitions": 1, "persons": 0, "users": 0} {
		var n int
		if err = db.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != want {
			t.Fatalf("%s: %d %v", table, n, err)
		}
	}
	if out, err := run("--confirm-empty-demo"); err == nil || !strings.Contains(out, "déjà des données") {
		t.Fatalf("repeat: %s %v", out, err)
	}
	if out, err := run("--wrong"); err == nil || !strings.Contains(out, "confirmation explicite") {
		t.Fatalf("CLI guard: %s %v", out, err)
	}
	var n int
	db.QueryRow(ctx, "SELECT count(*) FROM group_slots").Scan(&n)
	if n != 16 {
		t.Fatal(n)
	}
	// Canonical location changes flow through existing administrative reads.
	if _, err = db.Exec(ctx, "UPDATE locations SET name='Dojo renommé' WHERE id=$1", identity.Locations[0].ID); err != nil {
		t.Fatal(err)
	}
	adminSlots, err := q.AdministrativeSlots(ctx)
	if err != nil || len(adminSlots) != 16 || adminSlots[0].Location != "Dojo renommé" {
		t.Fatalf("administrative location: %v %v", adminSlots, err)
	}
	slots, err = service.Schedule(ctx, seasons[0].ID)
	if err != nil || slots[0].LocationName != "Dojo renommé" {
		t.Fatalf("rename: %v", err)
	}
	if _, err = db.Exec(ctx, "UPDATE locations SET is_active=false"); err != nil {
		t.Fatal(err)
	}
	slots, err = service.Schedule(ctx, seasons[0].ID)
	if err != nil || len(slots) != 0 {
		t.Fatalf("inactive location: %v %v", slots, err)
	}
	if _, err = db.Exec(ctx, "UPDATE organizations SET is_active=false"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Identity(ctx); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("inactive org: %v", err)
	}
}

func TestOrganizationConstraintsAndSeedGuard(t *testing.T) {
	db := newTestDatabase(t)
	ctx := t.Context()
	if err := demodata.SeedBudokan(ctx, db, true); !errors.Is(err, demodata.ErrGuard) {
		t.Fatalf("database guard: %v", err)
	}
	valid := []string{
		"INSERT INTO organizations(name) VALUES ('Autre association')",
		"INSERT INTO locations(organization_id,name,address) SELECT id,'Dojo','Adresse libre' FROM organizations",
		"INSERT INTO organization_links(organization_id,kind,label,url) SELECT id,'social','Réseau','https://example.org/account' FROM organizations",
	}
	for _, sql := range valid {
		if _, err := db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	invalid := []string{
		"INSERT INTO organizations(name) VALUES (' ')",
		"INSERT INTO organizations(name) VALUES ('Deuxième active')",
		"UPDATE organizations SET public_email='invalid'",
		"UPDATE organizations SET website_url='javascript:alert(1)'",
		"INSERT INTO locations(organization_id,name,address) VALUES (99999,'Lieu','Adresse')",
		"INSERT INTO locations(organization_id,name,address) SELECT id,'Dojo','Autre' FROM organizations",
		"UPDATE locations SET name=' '", "UPDATE locations SET address=' '",
		"UPDATE organization_links SET url='javascript:alert(1)'",
		"UPDATE organization_links SET kind=' '", "UPDATE organization_links SET position=-1",
		"INSERT INTO organization_links(organization_id,kind,label,url) SELECT organization_id,kind,label,url FROM organization_links",
		"DELETE FROM organizations",
	}
	for _, sql := range invalid {
		if _, err := db.Exec(ctx, sql); err == nil {
			t.Fatalf("accepted invalid write: %s", sql)
		}
	}
	if _, err := db.Exec(ctx, "UPDATE organizations SET is_active=false; INSERT INTO organizations(name) VALUES ('Deuxième association')"); err != nil {
		t.Fatal(err)
	}
}

func TestBudokanConcurrentSeedAndExistingReferences(t *testing.T) {
	db := newTestDatabaseNamed(t, "concurrent_demo")
	ctx := t.Context()
	// A populated season alone prevents all mutations, even without an organization.
	if _, err := db.Exec(ctx, "INSERT INTO seasons(name,starts_at,ends_at) VALUES ('2025/2026','2025-09-01','2026-08-31')"); err != nil {
		t.Fatal(err)
	}
	if err := demodata.SeedBudokan(ctx, db, true); !errors.Is(err, demodata.ErrNotEmpty) {
		t.Fatalf("existing reference: %v", err)
	}
	var name string
	if err := db.QueryRow(ctx, "SELECT name FROM seasons").Scan(&name); err != nil || name != "2025/2026" {
		t.Fatalf("preservation: %s %v", name, err)
	}
	// Remove only this test's own fixture before testing competing seeds.
	if _, err := db.Exec(ctx, "DELETE FROM seasons"); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- demodata.SeedBudokan(ctx, db, true) }()
	}
	successes, refused := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
		} else if errors.Is(err, demodata.ErrNotEmpty) {
			refused++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || refused != 1 {
		t.Fatalf("concurrency: %d / %d", successes, refused)
	}
	var slotID, locationID int32
	if err := db.QueryRow(ctx, "SELECT id,location_id FROM group_slots LIMIT 1").Scan(&slotID, &locationID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "UPDATE group_slots SET location='Duplicate text' WHERE id=$1", slotID); err == nil {
		t.Fatal("duplicate location accepted")
	}
	if _, err := db.Exec(ctx, "UPDATE group_slots SET location_id=999999 WHERE id=$1", slotID); err == nil {
		t.Fatal("missing location accepted")
	}
	if _, err := db.Exec(ctx, "DELETE FROM locations WHERE id=$1", locationID); err == nil {
		t.Fatal("referenced location deleted")
	}
	if _, err := db.Exec(ctx, "UPDATE organization_links SET is_active=false"); err != nil {
		t.Fatal(err)
	}
	links, err := dbsqlc.New(db).ListOrganizationLinks(ctx, 1)
	if err != nil || len(links) != 1 || links[0].IsActive {
		t.Fatalf("deactivated link: %v %v", links, err)
	}
}
