package handlers

import (
	"github.com/grapinou/club-core/internal/civildate"
	"github.com/grapinou/club-core/internal/database/dbtypes"
)

func frenchBirthDate(value string, optional bool) (dbtypes.Date, error) {
	if optional && value == "" {
		return dbtypes.Date{}, nil
	}
	t, err := civildate.ParseFrench(value)
	if err != nil {
		return dbtypes.Date{}, err
	}
	return dbtypes.Date{Time: t, Valid: true}, nil
}
