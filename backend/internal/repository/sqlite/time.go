package sqlite

import (
	"database/sql/driver"
	"fmt"
	"time"
)

type sqliteTimestamp time.Time

func (sqliteTimestamp) GormDataType() string { return "text" }

func (value sqliteTimestamp) Value() (driver.Value, error) {
	return formatTime(time.Time(value)), nil
}

func (value *sqliteTimestamp) Scan(source any) error {
	var parsed time.Time
	var err error
	switch source := source.(type) {
	case time.Time:
		parsed = source
	case string:
		parsed, err = parseTime(source)
	case []byte:
		parsed, err = parseTime(string(source))
	default:
		err = fmt.Errorf("unsupported SQLite timestamp value %T", source)
	}
	if err != nil {
		return err
	}
	*value = newSQLiteTimestamp(parsed)
	return nil
}

func newSQLiteTimestamp(value time.Time) sqliteTimestamp {
	return sqliteTimestamp(value.UTC())
}

func newNullableSQLiteTimestamp(value *time.Time) *sqliteTimestamp {
	if value == nil {
		return nil
	}
	timestamp := newSQLiteTimestamp(*value)
	return &timestamp
}

func (value sqliteTimestamp) Time() time.Time {
	return time.Time(value).UTC()
}

func timeFromNullableSQLiteTimestamp(value *sqliteTimestamp) *time.Time {
	if value == nil {
		return nil
	}
	timestamp := value.Time()
	return &timestamp
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
	} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("parse SQLite timestamp %q", value)
}
