package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/skudra-go/ant/schema"
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

// checkBody refuses what would make a migration run otherwise than it says,
// before it runs. It is checked when the migration is about to run, not when it
// is read, so a file applied before these checks still loads.
//
// A directive after the first statement is not read, so a Render that put a SET
// in front of the generated comments ran -- ant:no-transaction files in a
// transaction (#182). And ant runs each migration in a transaction of its own,
// which a COMMIT in the file ends: the statements before it stayed when a later
// one failed, though the error said the file was rolled back (#181).
func (m Migration) checkBody(d schema.Dialect) error {
	if m.run != nil {
		return nil
	}

	leading := true
	number := 0

	for line := range strings.Lines(m.SQL) {
		number++

		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}

		comment, ok := strings.CutPrefix(text, "--")
		if !ok {
			leading = false

			continue
		}

		// One the top already gives, as a squash that keeps the files has
		// inside, changes nothing.
		if fields := strings.Fields(comment); !leading && !m.noTransaction &&
			len(fields) > 0 && fields[0] == noTransactionDirective {
			return fmt.Errorf(
				"%w: %s: -- %s at line %d comes after the first statement, where it is not read: "+
					"move it into the comments at the top of the file",
				ErrBadMigration,
				m.ID(),
				fields[0],
				number,
			)
		}
	}

	if m.noTransaction {
		return nil
	}

	for _, statement := range d.Split(m.SQL) {
		if word := endsTransaction(statement); word != "" {
			return fmt.Errorf(
				"%w: %s has a %s of its own. ant runs each migration in a transaction, "+
					"which it would end: take out BEGIN and COMMIT, or put -- %s at the top "+
					"to run the file without one",
				ErrBadMigration,
				m.ID(),
				word,
				noTransactionDirective,
			)
		}
	}

	return nil
}

// endsTransaction returns the statement's first word if it starts or ends a
// transaction, and "" otherwise. ROLLBACK TO a savepoint stays inside one.
func endsTransaction(statement string) string {
	words := leadingWords(statement, 2)
	if len(words) == 0 {
		return ""
	}

	switch words[0] {
	case "BEGIN", "START", "COMMIT", "END", "ABORT":
		return words[0]
	case "ROLLBACK":
		if len(words) < 2 || words[1] != "TO" {
			return words[0]
		}
	}

	return ""
}

// leadingWords returns up to n words a statement starts with, upper-cased,
// after any comments before it.
func leadingWords(statement string, n int) []string {
	var out []string

	rest := statement

	for len(out) < n {
		rest = strings.TrimLeft(rest, " \t\r\n")

		switch {
		case strings.HasPrefix(rest, "--"):
			_, after, ok := strings.Cut(rest, "\n")
			if !ok {
				return out
			}

			rest = after

			continue
		case strings.HasPrefix(rest, "/*"):
			_, after, ok := strings.Cut(rest[2:], "*/")
			if !ok {
				return out
			}

			rest = after

			continue
		}

		end := strings.IndexFunc(rest, func(r rune) bool {
			return !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
		})

		if end == 0 {
			return out
		}

		if end < 0 {
			end = len(rest)
		}

		out = append(out, strings.ToUpper(rest[:end]))
		rest = rest[end:]
	}

	return out
}
