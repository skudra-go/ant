package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

// The default names follow PostgreSQL's own, so a table written by hand and one
// written by ant get the same constraint names.

// PrimaryKeyName returns the default name of a table's primary key.
func PrimaryKeyName(d Dialect, table string) string { return name(d, table, "pkey") }

// UniqueName returns the default name of a unique constraint.
func UniqueName(d Dialect, table string, columns ...string) string {
	return name(d, append(append([]string{table}, columns...), "key")...)
}

// IndexName returns the default name of an index.
func IndexName(d Dialect, table string, columns ...string) string {
	return name(d, append(append([]string{table}, columns...), "idx")...)
}

// ForeignKeyName returns the default name of a foreign key.
func ForeignKeyName(d Dialect, table string, columns ...string) string {
	return name(d, append(append([]string{table}, columns...), "fkey")...)
}

// name joins parts, and shortens a name past the dialect's limit, keeping it
// unique with a hash. The database would otherwise cut it silently, and two long
// names could become one.
func name(d Dialect, parts ...string) string {
	out := strings.Join(parts, "_")

	limit := d.MaxIdentifier()
	if limit == 0 || len(out) <= limit {
		return out
	}

	sum := sha256.Sum256([]byte(out))
	suffix := "_" + hex.EncodeToString(sum[:4])

	// Cut where a character starts: a cut through one left invalid UTF-8,
	// which PostgreSQL refuses in a name (#112).
	cut := limit - len(suffix)
	for cut > 0 && !utf8.RuneStart(out[cut]) {
		cut--
	}

	return out[:cut] + suffix
}
