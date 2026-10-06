package store

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"time"

	"github.com/jmoiron/sqlx"

	"gamematch/internal/config"
)

// Store is the data-access layer over a SQLite database.
type Store struct {
	DB  *sqlx.DB
	Cfg config.Config
}

// New wraps a database handle.
func New(db *sqlx.DB, cfg config.Config) *Store {
	return &Store{DB: db, Cfg: cfg}
}

// tsLayout is ISO8601 with microseconds, in UTC.
const tsLayout = "2006-01-02T15:04:05.000000Z07:00"

// NowTS is the current UTC time as a stored timestamp.
func NowTS() string { return time.Now().UTC().Format(tsLayout) }

// FmtTS formats a time as a stored/DTO timestamp.
func FmtTS(t time.Time) string { return t.UTC().Format(tsLayout) }

// ParseTS parses a stored timestamp.
func ParseTS(s string) time.Time {
	t, err := time.Parse(tsLayout, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

var phoneSpaceRE = regexp.MustCompile(`\s+`)

// NormalizePhone strips whitespace from a phone number.
func NormalizePhone(p string) string { return phoneSpaceRE.ReplaceAllString(p, "") }

// HashPhone returns the sha256 hex of a normalized phone.
func HashPhone(phone string) string {
	sum := sha256.Sum256([]byte(NormalizePhone(phone)))
	return hex.EncodeToString(sum[:])
}

// sqlxIn expands slice arguments for an IN (?) clause.
func sqlxIn(query string, args ...any) (string, []any, error) {
	return sqlx.In(query, args...)
}
