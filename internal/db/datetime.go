package db

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// rfc3339Time implements sql.Scanner for non-null TEXT datetimes stored in RFC3339.
// Use for created_at and other required datetime columns.
type rfc3339Time struct{ time.Time }

// Scan implements sql.Scanner. Accepts string, []byte, or nil (treated as zero time).
func (t *rfc3339Time) Scan(value any) error {
	if value == nil {
		t.Time = time.Time{}
		return nil
	}
	var s string
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("cannot scan %T into rfc3339Time", value)
	}
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}

// nullRFC3339Time implements sql.Scanner for nullable TEXT datetimes stored in RFC3339.
// Use for completed_at, hashed_at, etc.
type nullRFC3339Time struct {
	Time  time.Time
	Valid bool
}

// Scan implements sql.Scanner. Accepts string, []byte, time.Time (Postgres timestamptz), or nil (Valid = false).
func (n *nullRFC3339Time) Scan(value any) error {
	if value == nil {
		n.Valid = false
		n.Time = time.Time{}
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		n.Time = v
		n.Valid = true
		return nil
	case string:
		if v == "" {
			n.Valid = false
			n.Time = time.Time{}
			return nil
		}
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return err
		}
		n.Time = parsed
		n.Valid = true
		return nil
	case []byte:
		if len(v) == 0 {
			n.Valid = false
			n.Time = time.Time{}
			return nil
		}
		parsed, err := time.Parse(time.RFC3339, string(v))
		if err != nil {
			return err
		}
		n.Time = parsed
		n.Valid = true
		return nil
	default:
		return fmt.Errorf("cannot scan %T into nullRFC3339Time", value)
	}
}

// Ptr returns *time.Time for use in structs; returns nil if not Valid.
func (n *nullRFC3339Time) Ptr() *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time
	return &t
}

// Value implements driver.Valuer so the type can be used in Exec/Query args.
func (n nullRFC3339Time) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Time.UTC().Format(time.RFC3339), nil
}
