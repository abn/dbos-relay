package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func uuidToText(u pgtype.UUID) string {
	if !u.Valid {
		return uuid.NewString()
	}
	parsed, err := uuid.FromBytes(u.Bytes[:])
	if err != nil {
		return uuid.NewString()
	}
	return parsed.String()
}

func textToUUID(s string) pgtype.UUID {
	if s == "" {
		return pgtype.UUID{Valid: false}
	}
	parsed, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{Valid: false}
	}
	var b [16]byte
	copy(b[:], parsed[:])
	return pgtype.UUID{Bytes: b, Valid: true}
}

func timestamptzToText(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.UTC().Format(time.RFC3339Nano)
	return &s
}

func textToTimestamptz(ns sql.NullString) pgtype.Timestamptz {
	if !ns.Valid || ns.String == "" {
		return pgtype.Timestamptz{Valid: false}
	}
	// Try RFC3339Nano, RFC3339, or standard SQLite datetime formats
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, l := range layouts {
		if pt, err := time.Parse(l, ns.String); err == nil {
			return pgtype.Timestamptz{Time: pt.UTC(), Valid: true}
		}
	}
	return pgtype.Timestamptz{Valid: false}
}

func stringsToJSON(ss []string) string {
	if len(ss) == 0 {
		return "[]"
	}
	b, err := json.Marshal(ss)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func jsonToStrings(raw string) []string {
	if raw == "" || raw == "[]" {
		return []string{}
	}
	var res []string
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return []string{}
	}
	return res
}

func mapDBErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return pgx.ErrNoRows
	}
	return err
}
