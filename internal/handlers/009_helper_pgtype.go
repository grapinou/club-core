package handlers

import (
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func pgTypeDate(value string) (pgtype.Date, error) {

	t, err := time.Parse("2006-01-02", value)

	if err != nil {
		return pgtype.Date{}, fmt.Errorf(
			"La conversion de la date %q impossible : %w",
			value,
			err)
	}

	return pgtype.Date{
		Time:  t,
		Valid: true,
	}, nil

}

func pgTypeText(value string) pgtype.Text {
	value = strings.TrimSpace(value)

	if value == "" {
		return pgtype.Text{
			Valid: false,
		}
	}

	return pgtype.Text{
		String: value,
		Valid:  true,
	}
}
