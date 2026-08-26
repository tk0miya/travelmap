package sqlite

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// unixTime scans and stores a [time.Time] as the integer Unix seconds every
// timestamp column in this schema holds, per "Schema" in TODO.md, so a
// repository writes and reads a Go time directly rather than converting by
// hand on both sides of every column.
type unixTime time.Time

// Scan implements [sql.Scanner].
func (t *unixTime) Scan(src any) error {
	v, ok := src.(int64)
	if !ok {
		return fmt.Errorf("sqlite: scanning a Unix timestamp: %T is not an int64", src)
	}

	*t = unixTime(time.Unix(v, 0).UTC())

	return nil
}

// Value implements [driver.Valuer].
func (t unixTime) Value() (driver.Value, error) {
	return time.Time(t).Unix(), nil
}

// jsonStrings scans and stores a []string as the JSON array daily_stats'
// countries and cities columns hold. Empty encodes as "[]" rather than the
// "null" [json.Marshal] gives a nil slice, matching what those columns hold
// before reverse geocoding is enabled.
type jsonStrings []string

// Scan implements [sql.Scanner].
func (s *jsonStrings) Scan(src any) error {
	text, ok := src.(string)
	if !ok {
		return fmt.Errorf("sqlite: scanning a JSON string array: %T is not a string", src)
	}

	return json.Unmarshal([]byte(text), (*[]string)(s))
}

// Value implements [driver.Valuer].
func (s jsonStrings) Value() (driver.Value, error) {
	if len(s) == 0 {
		return "[]", nil
	}

	b, err := json.Marshal([]string(s))
	if err != nil {
		return nil, err
	}

	return string(b), nil
}

// collect runs the rows.Next/Scan/Err loop every multi-row query in this
// package shares: it closes rows, calls scan once per row, and reports
// whichever of scan's or [sql.Rows.Err]'s error comes first.
func collect[T any](rows *sql.Rows, scan func(*sql.Rows) (T, error)) ([]T, error) {
	defer rows.Close()

	var result []T

	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}

		result = append(result, v)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}
