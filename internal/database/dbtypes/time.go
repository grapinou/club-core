// Package dbtypes provides SQL values for civil dates, times of day and UTC
// timestamps. SQLite stores them as canonical text so ordering stays chronological.
package dbtypes

import (
	"database/sql/driver"
	"fmt"
	"time"
)

type Date struct {
	Time  time.Time
	Valid bool
}

func (d Date) IsFinite() bool { return !d.Time.IsZero() && d.Time.Year() >= 1 && d.Time.Year() <= 9999 }
func (d Date) Value() (driver.Value, error) {
	if !d.Valid {
		return nil, nil
	}
	if !d.IsFinite() {
		return nil, fmt.Errorf("invalid civil date")
	}
	return d.Time.Format("2006-01-02"), nil
}
func (d *Date) Scan(v any) error {
	if v == nil {
		*d = Date{}
		return nil
	}
	s, ok := v.(string)
	if t, yes := v.(time.Time); yes {
		s = t.Format("2006-01-02")
		ok = true
	}
	if !ok {
		return fmt.Errorf("date: unexpected %T", v)
	}
	t, e := time.Parse("2006-01-02", s)
	if e != nil {
		return e
	}
	*d = Date{Time: t, Valid: true}
	return nil
}

type Timestamp struct {
	Time  time.Time
	Valid bool
}

func (t Timestamp) Value() (driver.Value, error) {
	if !t.Valid {
		return nil, nil
	}
	return t.Time.UTC().Format("2006-01-02 15:04:05.000"), nil
}
func (t *Timestamp) Scan(v any) error {
	if v == nil {
		*t = Timestamp{}
		return nil
	}
	if x, ok := v.(time.Time); ok {
		*t = Timestamp{Time: x.UTC(), Valid: true}
		return nil
	}
	if s, ok := v.(string); ok {
		for _, layout := range []string{"2006-01-02 15:04:05.999999999", time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999 +0000 UTC"} {
			if x, e := time.Parse(layout, s); e == nil {
				*t = Timestamp{Time: x.UTC(), Valid: true}
				return nil
			}
		}
	}
	return fmt.Errorf("timestamp: unexpected %v (%T)", v, v)
}

type Time struct {
	Microseconds int64
	Valid        bool
}

func (t Time) Value() (driver.Value, error) {
	if !t.Valid {
		return nil, nil
	}
	if t.Microseconds < 0 || t.Microseconds >= 24*60*60*1000000 {
		return nil, fmt.Errorf("invalid time of day")
	}
	clock := time.Unix(0, t.Microseconds*1000).UTC()
	if t.Microseconds%60000000 == 0 {
		return clock.Format("15:04"), nil
	}
	return clock.Format("15:04:05.000000"), nil
}
func (t *Time) Scan(v any) error {
	if v == nil {
		*t = Time{}
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("time: unexpected %T", v)
	}
	for _, layout := range []string{"15:04:05.999999999", "15:04"} {
		if x, e := time.Parse(layout, s); e == nil {
			*t = Time{Microseconds: int64(x.Hour()*3600+x.Minute()*60+x.Second())*1000000 + int64(x.Nanosecond()/1000), Valid: true}
			return nil
		}
	}
	return fmt.Errorf("invalid time %q", s)
}

func (t Timestamp) IsFinite() bool {
	return !t.Time.IsZero() && t.Time.Year() >= 1 && t.Time.Year() <= 9999
}
