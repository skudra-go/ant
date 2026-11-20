package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
)

// Kind says how a migration runs.
type Kind string

// Migration kinds.
const (
	// Versioned migrations run once, in order.
	Versioned Kind = "versioned"
	// Repeatable migrations run after the versioned ones, and again whenever
	// they change or a versioned migration runs, since that migration may have
	// dropped what they define. They suit views and functions, and must be safe
	// to run again. A versioned migration cannot use what they define, since on
	// a new database they run after it.
	Repeatable Kind = "repeatable"
)

// Directive prefixes, read from comments at the top of a migration.
const (
	noTransactionDirective = "ant:no-transaction"
	replacesDirective      = "ant:replaces"
)

// Migration is one unit of schema change.
type Migration struct {
	Kind Kind
	// Version orders versioned migrations. It is empty for repeatable ones.
	Version string
	// Name is the descriptive part, such as add_bio.
	Name string
	// File is where the migration lives, relative to its folder. For a
	// repeatable migration, it is also its identity.
	File string
	// SQL is the migration's text. Comments at the top can hold directives:
	// -- ant:no-transaction runs it outside a transaction.
	SQL string

	run           func(ctx context.Context, db any, tx *sql.Tx) error // a Go migration
	noTransaction bool
	replaces      string
}

// ID identifies the migration in messages and in the snapshot.
func (m Migration) ID() string {
	if m.Kind == Repeatable {
		return m.File
	}

	return m.Version + "_" + m.Name
}

// Checksum identifies the migration's contents, whatever the line endings, so a
// checkout on Windows does not look like an edit. Go migrations have none.
func (m Migration) Checksum() string {
	if m.run != nil {
		return ""
	}

	sum := sha256.Sum256([]byte(strings.ReplaceAll(m.SQL, "\r\n", "\n")))

	return hex.EncodeToString(sum[:])
}

// key is the migration's identity in the history table.
func (m Migration) key() string {
	if m.Kind == Repeatable {
		return m.File
	}

	return m.Version
}

// readDirectives fills what the leading comments say.
func (m *Migration) readDirectives() error {
	for line := range strings.Lines(m.SQL) {
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}

		comment, ok := strings.CutPrefix(text, "--")
		if !ok {
			return nil
		}

		fields := strings.Fields(comment)
		if len(fields) == 0 {
			continue
		}

		switch {
		case fields[0] == noTransactionDirective:
			m.noTransaction = true
		case fields[0] == replacesDirective:
			if len(fields) != 2 {
				return fmt.Errorf(
					"%w: %s: want -- %s <version>",
					ErrBadMigration,
					m.ID(),
					replacesDirective,
				)
			}

			m.replaces = fields[1]
		case strings.HasPrefix(fields[0], "ant:"):
			// A misspelled directive would otherwise change how the file runs,
			// without a word.
			return fmt.Errorf("%w: %s: unknown directive %s", ErrBadMigration, m.ID(), fields[0])
		}
	}

	return nil
}
