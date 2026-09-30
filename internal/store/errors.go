package store

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsUniqueViolation reports whether err is a unique-constraint violation from
// either storage engine, so callers can map it to a conflict response.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed") ||
		strings.Contains(msg, "duplicate key value violates unique constraint")
}
