package ant

import (
	"errors"
	"fmt"

	"github.com/skudra-go/ant/internal/errs"
)

// Errors returned by terminal calls.
var (
	// ErrNotFound reports that a query for one row matched none.
	ErrNotFound = errors.New("ant: not found")
	// ErrMultipleRows reports that Single matched more than one row.
	ErrMultipleRows = errors.New("ant: query matched more than one row")
	// ErrNotImplemented reports a feature with no implementation yet.
	ErrNotImplemented = errs.ErrNotImplemented
	// ErrUnsupported reports something the connected database cannot do, such as
	// MERGE on MySQL. dialect.Supports asks in advance.
	ErrUnsupported = errs.ErrUnsupported
	// ErrNotProbe reports a field pointer that does not come from Fields.
	ErrNotProbe = errors.New("ant: pointer is not a field of a probe from ant.Fields")
	// ErrNotColumn reports a probe field that is a relation, used as a column.
	ErrNotColumn = errors.New("ant: field is a relation, not a column")
	// ErrNotRelation reports a probe field that is not a relation.
	ErrNotRelation = errors.New("ant: field is not a relation")
	// ErrWrongModel reports a probe from a model the query does not use.
	ErrWrongModel = errors.New("ant: field belongs to a model outside this query")
	// ErrNoContext reports a type passed to Open that does not embed Context.
	ErrNoContext = errors.New("ant: type does not embed ant.Context")
	// ErrNotOpen reports a table or context that did not come from Open.
	ErrNotOpen = errors.New("ant: context is not open")
	// ErrNoKey reports a key operation on a model with no primary key.
	ErrNoKey = errors.New("ant: model has no primary key")
	// ErrKeyType reports a key whose type does not match the primary key.
	ErrKeyType = errors.New("ant: key type does not match the primary key")
	// ErrZeroKey reports an insert of an entity whose key is its type's zero
	// value when nothing fills it: a uuid.UUID or string key with no
	// GenerateKey and no default. An integer key is never refused.
	ErrZeroKey = errors.New("ant: the key is zero and nothing generates it")
	// ErrNoForeignKey reports a relation whose foreign key cannot be found by
	// convention. Name it in Configure.
	ErrNoForeignKey = errs.ErrNoForeignKey
	// ErrNoColumnType reports a field whose database type cannot be worked out
	// from its Go type. Set it with Column(...).Type(...).
	ErrNoColumnType = errors.New("ant: no database type for field")
	// ErrBadSchema reports models that cannot become a valid schema.
	ErrBadSchema = errs.ErrBadSchema
	// ErrUnknownColumn reports a result column with no field, under WithStrict.
	ErrUnknownColumn = errors.New("ant: result column has no field")
	// ErrNoRows reports an operation given no rows to work on.
	ErrNoRows = errors.New("ant: no rows given")
	// ErrNilEntity reports a nil pointer passed to a write.
	ErrNilEntity = errors.New("ant: entity is nil")
	// ErrNoCondition reports a bulk write with no condition. Call Unfiltered
	// to affect every row on purpose.
	ErrNoCondition = errors.New("ant: bulk write has no condition")
	// ErrNoAssignment reports an Update with nothing to set.
	ErrNoAssignment = errors.New("ant: update has no assignments")
	// ErrThenWithoutOrder reports ThenBy on a query with no OrderBy.
	ErrThenWithoutOrder = errors.New("ant: ThenBy needs a preceding OrderBy")
	// ErrIncludeInEach reports Include or IncludeJoined with Each, which would
	// need a second query while rows stream. Use All, or Chunk for large results.
	ErrIncludeInEach = errors.New("ant: Each does not support Include")
	// ErrQueryInEach reports a statement inside an Each loop on a transaction,
	// or on a context from UseConn, whose one connection is still streaming the
	// loop's rows. Only SQLite can run it there: PostgreSQL, MySQL and MariaDB
	// fail, and SQL Server waits for rows nobody reads. Collect what the loop
	// needs and act after it, or read the rows with All or Chunk.
	ErrQueryInEach = errors.New(
		"ant: a statement cannot run while Each streams rows on the same connection; " +
			"finish the loop first, or read the rows with All or Chunk",
	)
	// ErrConcurrency reports that SaveChanges found a row changed or removed
	// since it was loaded. Nothing was saved.
	ErrConcurrency = errors.New("ant: row was changed or removed since it was loaded")
	// ErrConcurrentUse reports a context with change tracking used by two
	// operations at once. A tracking context is one unit of work: give each
	// request its own with Session, and share only a context with tracking
	// off, such as the one Open returns. A hook that queries must pass on the
	// context.Context it was given, which marks its query as part of the
	// operation that ran it.
	ErrConcurrentUse = errors.New(
		"ant: a tracking context was used by two operations at once; give each " +
			"goroutine its own ant.Session, share only contexts with tracking off, " +
			"and query from hooks with the context.Context they are given",
	)
	// ErrKeyChanged reports a tracked entity whose primary key was modified. A
	// key identifies the row, so use a new entity instead.
	ErrKeyChanged = errors.New("ant: primary key of a tracked entity was changed")
	// ErrCycle reports new entities that depend on each other, so neither can
	// be inserted first. Save them in two steps.
	ErrCycle = errors.New("ant: new entities depend on each other")
	// ErrNotTracked reports an operation on an entity the context is not
	// tracking.
	ErrNotTracked = errors.New("ant: entity is not tracked")
	// ErrNoTracking reports SaveChanges, or a helper that gives it work such as
	// Add, on a context with tracking off, such as the one Open returns, where
	// it would save nothing. A Session is a unit of work that tracks.
	ErrNoTracking = errors.New(
		"ant: this context does not track changes, so there is nothing to save; " +
			"use a session, ant.Session(db), for a unit of work",
	)
	// ErrNotInTx reports Commit, Rollback, or a savepoint outside a transaction.
	ErrNotInTx = errors.New("ant: not in a transaction")
	// ErrTxDone reports use of a transaction after it ended.
	ErrTxDone = errors.New("ant: transaction has ended")
	// ErrTxType reports a Tx closure whose parameter is not this context's type.
	ErrTxType = errors.New("ant: Tx closure takes a different context type")
	// ErrNestedIsolation reports Isolation on a Tx nested in another Tx. A
	// nested Tx is a savepoint, and the isolation level of a transaction cannot
	// change once it has begun, so the option would be dropped.
	ErrNestedIsolation = errors.New(
		"ant: Isolation cannot be set on a Tx nested in another Tx; set it on the outer Tx",
	)
	// ErrIncludeInSubquery reports a query with Include or IncludeJoined used as
	// a subquery, a member of a set operation, or the source of From, FromCTE,
	// Recursive, or JoinSub.
	ErrIncludeInSubquery = errors.New("ant: subquery cannot use Include")
	// ErrCursor reports a cursor that this query cannot use.
	ErrCursor = errors.New("ant: invalid cursor")
	// ErrPaging reports a page number, or a page or chunk size, below 1, or a
	// negative Limit or Offset. Such a value often comes from a request, as
	// ?page=0 does, so a handler can answer it as the client's mistake.
	ErrPaging = errors.New("ant: invalid paging")
	// ErrNotManyToMany reports a link operation on another kind of relation.
	ErrNotManyToMany = errors.New("ant: relation is not many-to-many")
	// ErrReadOnly reports a write to a read-only model, such as a view.
	ErrReadOnly = errors.New("ant: model is read-only")
	// ErrNotSoftDeleted reports Restore on a model with no DeletedAt field.
	ErrNotSoftDeleted = errors.New("ant: model has no DeletedAt field")
	// ErrNoColumns reports a query whose selection leaves no columns.
	ErrNoColumns = errors.New("ant: query selects no columns")
)

// ConcurrencyError is the ErrConcurrency from SaveChanges, naming the row.
type ConcurrencyError struct {
	Model string
	Key   any
}

// Error implements error.
func (e *ConcurrencyError) Error() string {
	return fmt.Sprintf("%v: %s %v", ErrConcurrency, e.Model, e.Key)
}

// Unwrap makes errors.Is(err, ErrConcurrency) true.
func (e *ConcurrencyError) Unwrap() error { return ErrConcurrency }

// ValidationError reports a field that failed a rule from ModelBuilder.Validate.
type ValidationError struct {
	Model string
	Field string
	Err   error
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("ant: %s.%s: %v", e.Model, e.Field, e.Err)
}

// Unwrap returns the rule's error.
func (e *ValidationError) Unwrap() error { return e.Err }
