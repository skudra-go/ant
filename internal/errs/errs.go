// Package errs holds sentinel errors shared by ant's packages.
package errs

import "errors"

// ErrNotImplemented reports a feature that exists in the API but has no
// implementation yet.
var ErrNotImplemented = errors.New("ant: not implemented")

// ErrUnsupported reports something the connected database cannot do.
var ErrUnsupported = errors.New("ant: not supported by this database")

// ErrNoForeignKey reports a relation whose foreign key cannot be determined.
var ErrNoForeignKey = errors.New("ant: relation has no foreign key")

// ErrBadSchema reports models that cannot become a valid schema.
var ErrBadSchema = errors.New("ant: invalid schema")
