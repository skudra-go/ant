package model_definition

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
)

// Tags that name opposite roles, or the same role twice, describe a field the
// model cannot mean. Each of these was accepted, and the model then did one of
// the two things the tags asked for without saying which.

type Unwritable struct {
	ID  int64
	Col string `ant:"readonly,writeonly"`
}

type UnwritableDB struct {
	ant.Context
	Unwritables ant.Table[Unwritable]
}

type TwoVersions struct {
	ID int64
	V1 int64 `ant:"version"`
	V2 int64 `ant:"version"`
}

type TwoVersionsDB struct {
	ant.Context
	TwoVersions ant.Table[TwoVersions]
}

type ComputedKey struct {
	ID  int64
	Val string `ant:"computed,unique"`
}

type ComputedKeyDB struct {
	ant.Context
	ComputedKeys ant.Table[ComputedKey]
}

func TestConflictingTagsAreRefused(t *testing.T) {
	_, err := ant.Wrap[UnwritableDB](postgres.Dialect, nil)
	require.ErrorContains(t, err, "readonly")

	_, err = ant.Wrap[TwoVersionsDB](postgres.Dialect, nil)
	require.ErrorContains(t, err, "version")

	_, err = ant.Wrap[ComputedKeyDB](postgres.Dialect, nil)
	require.ErrorContains(t, err, "computed")
}

// A field the model does not use has no column, so its tags describe nothing
// and the check must leave it alone.
type Ignored struct {
	ID   int64
	Both string `ant:"-,readonly,writeonly"`
}

type IgnoredDB struct {
	ant.Context
	Ignoreds ant.Table[Ignored]
}

func TestIgnoredFieldsKeepTheirOtherTags(t *testing.T) {
	_, err := ant.Wrap[IgnoredDB](postgres.Dialect, nil)
	require.NoError(t, err)
}
