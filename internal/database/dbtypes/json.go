package dbtypes

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// Strings is a JSON list used for ordered read projections and organization copy.
type Strings []string

func (s *Strings) Scan(v any) error {
	if v == nil {
		*s = nil
		return nil
	}
	switch x := v.(type) {
	case string:
		return json.Unmarshal([]byte(x), s)
	case []byte:
		return json.Unmarshal(x, s)
	}
	return fmt.Errorf("JSON list: unexpected %T", v)
}
func (s Strings) Value() (driver.Value, error) {
	if s == nil {
		s = Strings{}
	}
	v, e := json.Marshal(s)
	return string(v), e
}

type IDs []int32

func (s *IDs) Scan(v any) error {
	if v == nil {
		*s = nil
		return nil
	}
	switch x := v.(type) {
	case string:
		return json.Unmarshal([]byte(x), s)
	case []byte:
		return json.Unmarshal(x, s)
	}
	return fmt.Errorf("JSON IDs: unexpected %T", v)
}

func (s IDs) Value() (driver.Value, error) {
	if s == nil {
		s = IDs{}
	}
	v, e := json.Marshal(s)
	return string(v), e
}
