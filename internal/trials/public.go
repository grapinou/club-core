package trials

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"database/sql"

	"github.com/grapinou/club-core/internal/civildate"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/database/dbtypes"
)

type PublicService struct {
	db     *sql.DB
	trials *Service
	loc    *time.Location
}

func NewPublic(db *sql.DB, loc *time.Location) *PublicService {
	return &PublicService{db, New(db), loc}
}

type PublicOffering struct {
	ActivityID, GroupID, SlotID                              int32
	Activity, Group, Practice, Start, End, Location, Address string
	Weekday                                                  int
	Dates                                                    []string
}
type PublicBooking struct {
	Offering                                                                        PublicOffering
	Date                                                                            string
	FirstName, LastName, BirthDate, Email, Phone                                    string
	Minor                                                                           bool // Deprecated: ignored; BirthDate is authoritative.
	GuardianFirstName, GuardianLastName, GuardianEmail, GuardianPhone, Relationship string
	EquipmentNeeded                                                                 bool
	EquipmentDetails                                                                string
}
type PublicPerson struct{ FirstName, LastName, BirthDate, Email, Phone string }
type PublicConfirmation struct {
	TrialID          int32
	Offering         PublicOffering
	Date             string
	FirstName        string
	Registrant       PublicPerson
	Guardian         *PublicPerson
	EquipmentNeeded  bool
	EquipmentDetails string
	EmailTo          string
}

var ErrInvalidPublicBooking = errors.New("invalid public booking")

const publicOfferingsSQL = `SELECT a.id,g.id,gs.id,a.name,g.name,coalesce(gs.practice_label,''),substr(gs.start_time,1,5),substr(gs.end_time,1,5),coalesce(l.name,gs.location,''),coalesce(l.address,''),gs.weekday,gs.valid_from,gs.valid_until,s.starts_at,s.ends_at,gs.start_time
FROM group_slots gs JOIN groups g ON g.id=gs.group_id JOIN activities a ON a.id=g.activity_id JOIN seasons s ON s.id=gs.season_id
LEFT JOIN locations l ON l.id=gs.location_id
WHERE a.is_active AND g.is_active AND g.show_name_publicly AND gs.is_active AND s.is_active
AND (gs.location_id IS NULL OR (l.is_active AND EXISTS(SELECT 1 FROM organizations o WHERE o.id=l.organization_id AND o.is_active)))
AND EXISTS(SELECT 1 FROM organizations WHERE is_active)
AND s.starts_at<=?2 AND s.ends_at>=?1
AND gs.valid_from<=?2 AND (gs.valid_until IS NULL OR gs.valid_until>=?1)
ORDER BY a.name,g.name,gs.weekday,gs.start_time,gs.id`

// Offerings are a short rolling list. The final write rechecks the selected
// target in the same transaction as the people and trial.
func (s *PublicService) Offerings(ctx context.Context, now time.Time) ([]PublicOffering, error) {
	first, last := PublicWindow(now, s.loc)
	rows, err := s.db.QueryContext(ctx, publicOfferingsSQL, dbtypes.Date{Time: first, Valid: true}, dbtypes.Date{Time: last, Valid: true})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublicOffering
	for rows.Next() {
		var o PublicOffering
		var from, start, end time.Time
		var sessionStart string
		var until dbtypes.Date
		if err = rows.Scan(&o.ActivityID, &o.GroupID, &o.SlotID, &o.Activity, &o.Group, &o.Practice, &o.Start, &o.End, &o.Location, &o.Address, &o.Weekday, &from, &until, &start, &end, &sessionStart); err != nil {
			return nil, err
		}
		for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
			if int(d.Weekday()+6)%7+1 != o.Weekday || d.Before(from) || d.Before(start) || d.After(end) || (until.Valid && d.After(until.Time)) {
				continue
			}
			if !sessionAhead(d, sessionStart, now, s.loc) {
				continue
			}
			o.Dates = append(o.Dates, d.Format("2006-01-02"))
		}
		if len(o.Dates) > 0 {
			out = append(out, o)
		}
	}
	return out, rows.Err()
}
func validName(s string) bool { return s != "" && utf8.RuneCountInString(s) <= 100 }
func validEmail(s string) bool {
	if len(s) > 254 {
		return false
	}
	a, e := mail.ParseAddress(s)
	return e == nil && a.Address == s
}
func validPhone(s string) bool          { return s != "" && utf8.RuneCountInString(s) <= 40 }
func textValue(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
func (s *PublicService) Book(ctx context.Context, b PublicBooking, now time.Time) (PublicConfirmation, error) {
	var result PublicConfirmation
	b.FirstName = strings.TrimSpace(b.FirstName)
	b.LastName = strings.TrimSpace(b.LastName)
	b.Email = strings.TrimSpace(b.Email)
	b.Phone = strings.TrimSpace(b.Phone)
	b.GuardianFirstName = strings.TrimSpace(b.GuardianFirstName)
	b.GuardianLastName = strings.TrimSpace(b.GuardianLastName)
	b.GuardianEmail = strings.TrimSpace(b.GuardianEmail)
	b.GuardianPhone = strings.TrimSpace(b.GuardianPhone)
	b.EquipmentDetails = strings.Join(strings.Fields(b.EquipmentDetails), " ")
	if !b.EquipmentNeeded {
		b.EquipmentDetails = ""
	}
	if len(b.EquipmentDetails) > 500 {
		return result, ErrInvalidPublicBooking
	}
	birth, e := time.Parse("2006-01-02", b.BirthDate)
	date, ed := time.Parse("2006-01-02", b.Date)
	if e != nil || ed != nil || !validName(b.FirstName) || !validName(b.LastName) || b.Offering.ActivityID <= 0 || b.Offering.GroupID <= 0 || b.Offering.SlotID <= 0 {
		return result, ErrInvalidPublicBooking
	}
	today := now.In(s.loc)
	civilToday := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	first, last := PublicWindow(now, s.loc)
	if birth.After(civilToday) || birth.Before(civilToday.AddDate(-120, 0, 0)) || date.Before(first) || date.After(last) {
		return result, ErrInvalidPublicBooking
	}
	minor := civildate.IsMinor(birth, civilToday)
	if minor {
		if !validName(b.GuardianFirstName) || !validName(b.GuardianLastName) || !validEmail(b.GuardianEmail) || !validPhone(b.GuardianPhone) || !validRelationship(b.Relationship) {
			return result, ErrInvalidPublicBooking
		}
	} else if !validEmail(b.Email) || !validPhone(b.Phone) {
		return result, ErrInvalidPublicBooking
	}
	// BirthDate is authoritative: never retain coordinates from the other path.
	if minor {
		b.Email, b.Phone = "", ""
	} else {
		b.GuardianFirstName, b.GuardianLastName, b.GuardianEmail, b.GuardianPhone, b.Relationship = "", "", "", "", ""
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	// Acquire the policy before the calendar, matching office/configuration lock order.
	if _, err = lockPolicy(ctx, tx); err != nil {
		return result, err
	}
	// Lock the selected slot and its calendar before creating any person.
	var active bool
	var group, activity int32
	var location, addr, groupName, activityName, practice, start, end, sessionStart string
	err = tx.QueryRowContext(ctx, `SELECT gs.is_active AND g.is_active AND g.show_name_publicly AND a.is_active AND s.is_active AND ?4 BETWEEN gs.valid_from AND coalesce(gs.valid_until,s.ends_at) AND ?4 BETWEEN s.starts_at AND s.ends_at AND ((CAST(strftime('%w',?4) AS INTEGER)+6)%7+1)=gs.weekday AND (gs.location_id IS NULL OR (l.is_active AND o.is_active)) AS valid,
 g.id,a.id,coalesce(l.name,gs.location,''),coalesce(l.address,''),g.name,a.name,coalesce(gs.practice_label,''),substr(gs.start_time,1,5),substr(gs.end_time,1,5),gs.start_time
 FROM group_slots gs JOIN groups g ON g.id=gs.group_id JOIN activities a ON a.id=g.activity_id JOIN seasons s ON s.id=gs.season_id LEFT JOIN locations l ON l.id=gs.location_id LEFT JOIN organizations o ON o.id=l.organization_id
 WHERE gs.id=?1 AND g.id=?2 AND a.id=?3 AND EXISTS(SELECT 1 FROM organizations WHERE is_active)`, b.Offering.SlotID, b.Offering.GroupID, b.Offering.ActivityID, dbtypes.Date{Time: date, Valid: true}).Scan(&active, &group, &activity, &location, &addr, &groupName, &activityName, &practice, &start, &end, &sessionStart)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !active {
		return result, ErrInvalidPublicBooking
	}
	if err != nil {
		return result, err
	}
	if !sessionAhead(date, sessionStart, now, s.loc) {
		return result, ErrInvalidPublicBooking
	}
	var equipmentPrompt sql.NullString
	if err = tx.QueryRowContext(ctx, "SELECT trial_equipment_detail_prompt FROM organizations WHERE is_active").Scan(&equipmentPrompt); err != nil {
		return result, err
	}
	if b.EquipmentNeeded && equipmentPrompt.Valid && equipmentPrompt.String != "" && b.EquipmentDetails == "" {
		return result, ErrInvalidPublicBooking
	}
	q := dbsqlc.New(tx)
	person, err := q.CreatePerson(ctx, dbsqlc.CreatePersonParams{FirstName: b.FirstName, LastName: b.LastName, BirthDate: dbtypes.Date{Time: birth, Valid: true}, Email: textValue(b.Email), PhoneNumber: textValue(b.Phone)})
	if err != nil {
		return result, err
	}
	if minor {
		guardian, err := q.CreatePerson(ctx, dbsqlc.CreatePersonParams{FirstName: b.GuardianFirstName, LastName: b.GuardianLastName, Email: textValue(b.GuardianEmail), PhoneNumber: textValue(b.GuardianPhone)})
		if err != nil {
			return result, err
		}
		_, err = q.CreatePersonGuardian(ctx, dbsqlc.CreatePersonGuardianParams{ChildPersonID: person.ID, GuardianPersonID: guardian.ID, RelationshipType: b.Relationship, IsPrimaryContact: true})
		if err != nil {
			return result, err
		}
	}
	var notes sql.NullString
	if b.EquipmentNeeded {
		notes = sql.NullString{String: "Matériel demandé pour l’essai : oui", Valid: true}
		if b.EquipmentDetails != "" {
			notes.String += ". Précision : " + b.EquipmentDetails
		}
	}
	trial, err := s.trials.ScheduleTx(ctx, tx, dbsqlc.CreateTrialParams{PersonID: person.ID, ActivityID: activity, GroupID: sql.NullInt32{Int32: group, Valid: true}, GroupSlotID: sql.NullInt32{Int32: b.Offering.SlotID, Valid: true}, TrialDate: dbtypes.Date{Time: date, Valid: true}, Notes: notes})
	if err != nil {
		return result, fmt.Errorf("schedule public trial: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	result = PublicConfirmation{TrialID: trial.ID, Date: b.Date, FirstName: b.FirstName, Registrant: PublicPerson{b.FirstName, b.LastName, b.BirthDate, b.Email, b.Phone}, EquipmentNeeded: b.EquipmentNeeded, EquipmentDetails: b.EquipmentDetails,
		Offering: PublicOffering{ActivityID: activity, GroupID: group, SlotID: b.Offering.SlotID, Activity: activityName, Group: groupName, Practice: practice, Start: start, End: end, Location: location, Address: addr}}
	if minor {
		result.Guardian = &PublicPerson{FirstName: b.GuardianFirstName, LastName: b.GuardianLastName, Email: b.GuardianEmail, Phone: b.GuardianPhone}
		result.EmailTo = b.GuardianEmail
	} else {
		result.EmailTo = b.Email
	}
	return result, nil
}
func validRelationship(s string) bool {
	return s == "mother" || s == "father" || s == "guardian" || s == "other"
}

// PublicWindow returns civil dates: today through today + 21 inclusive.
func PublicWindow(now time.Time, loc *time.Location) (time.Time, time.Time) {
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	return today, today.AddDate(0, 0, 21)
}

// The date is civil UTC storage; construct the session instant in business time.
// The complete start time is used, including seconds, rather than the UI label.
func sessionAhead(date time.Time, start string, now time.Time, loc *time.Location) bool {
	clock, err := time.Parse("15:04:05", start)
	if err != nil {
		clock, err = time.Parse("15:04", start)
	}
	if err != nil {
		return false
	}
	session := time.Date(date.Year(), date.Month(), date.Day(), clock.Hour(), clock.Minute(), clock.Second(), clock.Nanosecond(), loc)
	return session.After(now)
}
