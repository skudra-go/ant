package model_definition

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
)

// Tags that name opposite roles, or the same role twice, describe a field the
// model cannot mean. Each of these was accepted, and the model then did one of
// the two things the tags asked for without saying which: readonly,writeonly
// gave a NOT NULL column that was never written, so every insert failed (#208).

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
	require.ErrorIs(t, err, ant.ErrBadSchema)

	require.ErrorContains(
		t,
		err,
		"Unwritable.Col: readonly and writeonly can't go together: the field would never be written or read",
	)

	_, err = ant.Wrap[TwoVersionsDB](postgres.Dialect, nil)
	require.ErrorIs(t, err, ant.ErrBadSchema)
	require.ErrorContains(t, err, "TwoVersions.V1 and TwoVersions.V2 are both version fields")

	_, err = ant.Wrap[ComputedKeyDB](postgres.Dialect, nil)
	require.ErrorIs(t, err, ant.ErrBadSchema)
	require.ErrorContains(t, err, "ComputedKey.Val: computed and unique can't go together")
}

type VersionReadonly struct {
	ID int64
	V  int64 `ant:"version,readonly"`
}

type VersionWriteonly struct {
	ID int64
	V  int64 `ant:"version,writeonly"`
}

type VersionImmutable struct {
	ID int64
	V  int64 `ant:"version,immutable"`
}

type VersionComputed struct {
	ID int64
	V  int64 `ant:"version,computed"`
}

type TokenWriteonly struct {
	ID int64
	T  string `ant:"token,writeonly"`
}

type TokenComputed struct {
	ID int64
	T  string `ant:"token,computed"`
}

type ReadonlyImmutable struct {
	ID  int64
	Col string `ant:"readonly,immutable"`
}

type ComputedWriteonly struct {
	ID  int64
	Col string `ant:"computed,writeonly"`
}

type ComputedImmutable struct {
	ID  int64
	Col string `ant:"computed,immutable"`
}

type ComputedDefault struct {
	ID  int64
	Col string `ant:"computed,default=1"`
}

// GeneratedWriteonly is readonly by its configuration, not its tag: a generated
// column is the database's to write.
type GeneratedWriteonly struct {
	ID    int64
	Name  string
	Upper string `ant:"writeonly"`
}

var GeneratedWriteonlys = ant.Fields[GeneratedWriteonly]()

func (GeneratedWriteonly) Configure(b *ant.ModelBuilder[GeneratedWriteonly]) {
	b.Column(&GeneratedWriteonlys.Upper).Generated("upper(name)")
}

// ComputedConfigured is computed by its tag and unique by its configuration.
type ComputedConfigured struct {
	ID  int64
	Col string `ant:"computed"`
}

var ComputedConfigureds = ant.Fields[ComputedConfigured]()

func (ComputedConfigured) Configure(b *ant.ModelBuilder[ComputedConfigured]) {
	b.Column(&ComputedConfigureds.Col).Unique()
}

type Place struct {
	Street string
}

type UniqueEmbedded struct {
	ID   int64
	Home Place `ant:"embedded,unique"`
}

type ReadonlyAnonymous struct {
	Place `ant:"readonly"`

	ID int64
}

type TwoDeletedAt struct {
	ID        int64
	DeletedAt ant.DeletedAt
	RemovedAt ant.DeletedAt
}

type ConflictsDB struct {
	ant.Context
	VersionReadonlys    ant.Table[VersionReadonly]
	VersionWriteonlys   ant.Table[VersionWriteonly]
	VersionImmutables   ant.Table[VersionImmutable]
	VersionComputeds    ant.Table[VersionComputed]
	TokenWriteonlys     ant.Table[TokenWriteonly]
	TokenComputeds      ant.Table[TokenComputed]
	ReadonlyImmutables  ant.Table[ReadonlyImmutable]
	ComputedWriteonlys  ant.Table[ComputedWriteonly]
	ComputedImmutables  ant.Table[ComputedImmutable]
	ComputedDefaults    ant.Table[ComputedDefault]
	GeneratedWriteonlys ant.Table[GeneratedWriteonly]
	ComputedConfigureds ant.Table[ComputedConfigured]
	UniqueEmbeddeds     ant.Table[UniqueEmbedded]
	ReadonlyAnonymouses ant.Table[ReadonlyAnonymous]
	TwoDeletedAts       ant.Table[TwoDeletedAt]
}

// Every pair that asks for opposite things is refused, whether the tags or the
// configuration give them, and Open names each model that has one.
func TestEveryConflictIsRefused(t *testing.T) {
	_, err := ant.Wrap[ConflictsDB](postgres.Dialect, nil)
	require.ErrorIs(t, err, ant.ErrBadSchema)

	for _, want := range []string{
		"VersionReadonly.V: version and readonly can't go together: ant writes the version on every update",
		"VersionWriteonly.V: version and writeonly can't go together: ant reads the version back to compare it",
		"VersionImmutable.V: version and immutable can't go together",
		"VersionComputed.V: version and computed can't go together",
		"TokenWriteonly.T: token and writeonly can't go together: ant reads the token back to compare it",
		"TokenComputed.T: computed and token can't go together",
		"ReadonlyImmutable.Col: readonly and immutable can't go together: immutable is written on insert, and readonly never is",
		"ComputedWriteonly.Col: computed and writeonly can't go together",
		"ComputedImmutable.Col: computed and immutable can't go together",
		"ComputedDefault.Col: computed and default can't go together",
		"GeneratedWriteonly.Upper: a generated column and writeonly can't go together",
		"ComputedConfigured.Col: computed and unique can't go together",
		"UniqueEmbedded.Home: an embedded struct has no column of its own for unique",
		"ReadonlyAnonymous.Place: an embedded struct has no column of its own for readonly",
		"TwoDeletedAt.DeletedAt and TwoDeletedAt.RemovedAt are both ant.DeletedAt fields",
	} {
		require.ErrorContains(t, err, want)
	}
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

// Keys that only repeat each other, or ask for things that fit together, still
// open: a version that is also a token, a token the database keeps, a value
// written once and never read back, and a key given twice.
type Compatible struct {
	ID       int64
	Revision int64  `ant:"version,token,version"`
	Stamp    string `ant:"readonly,token"`
	Secret   string `ant:"writeonly,immutable"`
	Code     string `ant:"readonly,unique,default=gen_code()"`
	Score    int    `ant:"computed,readonly"`
	Work     Place  `ant:"embedded,prefix=work_"`
}

type CompatibleDB struct {
	ant.Context
	Compatibles ant.Table[Compatible]
}

func TestCompatibleTagsOpen(t *testing.T) {
	_, err := ant.Wrap[CompatibleDB](postgres.Dialect, nil)
	require.NoError(t, err)
}
