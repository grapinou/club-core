package handlers

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/grapinou/club-core/internal/database/dbtypes"
)

func pgTypeDate(value string) (dbtypes.Date, error) {

	t, err := time.Parse("2006-01-02", value)

	if err != nil {
		return dbtypes.Date{}, fmt.Errorf(
			"La conversion de la date %q impossible : %w",
			value,
			err)
	}

	return dbtypes.Date{
		Time:  t,
		Valid: true,
	}, nil

}

func pgTypeText(value string) sql.NullString {
	value = strings.TrimSpace(value)

	if value == "" {
		return sql.NullString{
			Valid: false,
		}
	}

	return sql.NullString{
		String: value,
		Valid:  true,
	}
}
