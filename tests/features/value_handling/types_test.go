package value_handling

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/secure"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// Blob stores a Go value with encoding/gob, for what only Go reads.
type Blob struct {
	ID   int64
	Data ant.Gob[map[string]int]
}

// Stamped keeps its times as Unix seconds, and they are maintained all the same.
type Stamped struct {
	ID        int64
	Name      string
	CreatedAt ant.UnixTime
	UpdatedAt ant.UnixTime
}

// Patient holds an encrypted value.
type Patient struct {
	ID  int64
	SSN secure.Value[string]
}

// Doc has a JSON document.
type Doc struct {
	ID   int64
	Meta ant.JSON[map[string]any]
}

// TypesDB holds them.
type TypesDB struct {
	ant.Context
	Blobs    ant.Table[Blob]
	Stamped  ant.Table[Stamped]
	Patients ant.Table[Patient]
	Docs     ant.Table[Doc]
}

// Probes.
var (
	ST = ant.Fields[Stamped]()
	DC = ant.Fields[Doc]()
)

const typesDDL = `
	CREATE TABLE blobs (id BIGSERIAL PRIMARY KEY, data BYTEA NOT NULL);
	CREATE TABLE stampeds (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL);
	CREATE TABLE patients (id BIGSERIAL PRIMARY KEY, ssn TEXT NOT NULL);
	CREATE TABLE docs (id BIGSERIAL PRIMARY KEY, meta JSONB NOT NULL)`

func TestStoredTypes(t *testing.T) {
	fixture.RunTrackedWith(t, typesDDL, func(t *testing.T, db *TypesDB) {
		ctx := t.Context()

		t.Run("gob", func(t *testing.T) {
			row := &Blob{Data: ant.Gob[map[string]int]{V: map[string]int{"a": 1}}}
			require.NoError(t, db.Blobs.Insert(ctx, row))
			loaded, err := db.Blobs.NoTracking().First(ctx)
			require.NoError(t, err)
			require.Equal(t, 1, loaded.Data.V["a"])
		})

		t.Run("Unix timestamps are stamped like times", func(t *testing.T) {
			row := &Stamped{Name: "x"}
			require.NoError(t, db.Stamped.Insert(ctx, row))
			loaded, err := db.Stamped.NoTracking().First(ctx)
			require.NoError(t, err)
			require.WithinDuration(t, time.Now(), loaded.CreatedAt.Time, time.Minute)

			raw, err := ant.Raw[int64](db, "SELECT created_at FROM stampeds").First(ctx)
			require.NoError(t, err)
			require.Greater(t, *raw, int64(1_000_000_000), "an integer in the database")

			_, err = db.Stamped.Where(ant.Eq(&ST.Name, "x")).Update(ctx, ant.Set(&ST.Name, "y"))
			require.NoError(t, err, "bulk updates stamp it too")
		})

		t.Run("encrypted values read back, and are not in the table", func(t *testing.T) {
			key := make([]byte, 32)
			require.NoError(t, secure.SetKeys("k1", map[string][]byte{"k1": key}))
			require.NoError(t, db.Patients.Insert(ctx, &Patient{SSN: secure.New("123-45-6789")}))
			loaded, err := db.Patients.NoTracking().First(ctx)
			require.NoError(t, err)
			require.Equal(t, "123-45-6789", loaded.SSN.V)

			raw, err := ant.Raw[string](db, "SELECT ssn FROM patients").First(ctx)
			require.NoError(t, err)
			require.NotContains(t, *raw, "123")
		})

		t.Run("JSONSet updates inside a document", func(t *testing.T) {
			row := &Doc{
				Meta: ant.JSON[map[string]any]{V: map[string]any{"keep": true, "level": 1.0}},
			}
			require.NoError(t, db.Docs.Insert(ctx, row))

			_, err := db.Docs.Where(ant.Eq(&DC.ID, row.ID)).
				Update(ctx, ant.SetExpr(&DC.Meta, postgres.JSONSet(&DC.Meta, 2, "level")))
			require.NoError(t, err)
			loaded, err := db.Docs.NoTracking().Find(ctx, row.ID)
			require.NoError(t, err)
			require.Equal(t, 2.0, loaded.Meta.V["level"])
			require.Equal(t, true, loaded.Meta.V["keep"])
		})
	})
}

// ant.JSON marshals as the document it holds, so a model can go straight into an
// API response.
func TestJSONMarshalsAsItsValue(t *testing.T) {
	text, err := json.Marshal(ant.JSON[map[string]int]{V: map[string]int{"a": 1}})
	require.NoError(t, err)
	require.JSONEq(t, `{"a": 1}`, string(text))

	var back ant.JSON[map[string]int]
	require.NoError(t, json.Unmarshal([]byte(`{"b": 2}`), &back))
	require.Equal(t, 2, back.V["b"])
}

// A named key type works as a key, in Find and relations alike.
type OrderID int64

// Order has a value object for a key.
type Order struct {
	ID    OrderID
	Total int
}

// KeyDB holds it.
type KeyDB struct {
	ant.Context
	Orders ant.Table[Order]
}

func TestValueObjectKeys(t *testing.T) {
	const ddl = `CREATE TABLE orders (id BIGSERIAL PRIMARY KEY, total INT NOT NULL)`
	fixture.RunTrackedWith(t, ddl, func(t *testing.T, db *KeyDB) {
		ctx := t.Context()
		order := &Order{Total: 5}
		require.NoError(t, db.Orders.Insert(ctx, order))
		require.NotZero(t, order.ID)
		require.NoError(t, db.ClearTracked(ctx))
		loaded, err := db.Orders.Find(ctx, order.ID)
		require.NoError(t, err)
		require.Equal(t, 5, loaded.Total)
	})
}

// Place has a location in PostgreSQL's built-in point type.
type Place struct {
	ID   int64
	Name string
	At   postgres.Point
}

// PL is Place's probe.
var PL = ant.Fields[Place]()

// PlaceDB holds it.
type PlaceDB struct {
	ant.Context
	Places ant.Table[Place]
}

func TestPoints(t *testing.T) {
	const ddl = `CREATE TABLE places (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, at POINT NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *PlaceDB) {
		ctx := t.Context()
		require.NoError(t, db.Places.Insert(ctx,
			&Place{Name: "near", At: postgres.Point{X: 1, Y: 1}},
			&Place{Name: "far", At: postgres.Point{X: 50, Y: 50}}))

		found, err := db.Places.Where(postgres.Near(&PL.At, postgres.Point{}, 5)).All(ctx)
		require.NoError(t, err)
		require.Len(t, found, 1)
		require.Equal(t, postgres.Point{X: 1, Y: 1}, found[0].At)

		byDistance := postgres.Distance(&PL.At, postgres.Point{X: 49, Y: 49}).Asc()

		ordered, err := db.Places.Order(byDistance).
			First(ctx)
		require.NoError(t, err)
		require.Equal(t, "far", ordered.Name)
	})
}

// A key with a comma stays one key, whatever a path comes from.
func TestJSONPathsAreQuoted(t *testing.T) {
	fixture.RunTrackedWith(t, typesDDL, func(t *testing.T, db *TypesDB) {
		ctx := t.Context()
		row := &Doc{Meta: ant.JSON[map[string]any]{V: map[string]any{"a,b": "found"}}}
		require.NoError(t, db.Docs.Insert(ctx, row))

		value, err := ant.Project[DocValue](db.Docs.Query(),
			ant.IntoExpr(&DV.Value, postgres.JSONText(&DC.Meta, "a,b"))).First(ctx)
		require.NoError(t, err)
		require.Equal(t, "found", value.Value)
	})
}

// DocValue is one value read from a document.
type DocValue struct{ Value string }

// DV is DocValue's probe.
var DV = ant.Fields[DocValue]()

// Embedded holds a pgvector column.
type Embedded struct {
	ID        int64
	Embedding postgres.Vector
}

// EM is Embedded's probe.
var EM = ant.Fields[Embedded]()

// VectorDB holds it.
type VectorDB struct {
	ant.Context
	Rows ant.Table[Embedded]
}

// Vectors read and write in pgvector's format, and similarity orders rows. Without
// the extension in the database, the test skips.
func TestVectors(t *testing.T) {
	var round postgres.Vector
	require.NoError(t, round.Scan("[1,2.5,3]"))
	require.Equal(t, postgres.Vector{1, 2.5, 3}, round)
	text, err := round.Value()
	require.NoError(t, err)
	require.Equal(t, "[1,2.5,3]", text)

	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		ctx := t.Context()
		if _, err := target.DB.ExecContext(
			ctx,
			"CREATE EXTENSION IF NOT EXISTS vector",
		); err != nil {
			t.Skip("pgvector is not installed")
		}

		_, err := target.DB.ExecContext(
			ctx,
			"CREATE TABLE embeddeds (id BIGSERIAL PRIMARY KEY, embedding vector(2) NOT NULL)",
		)
		require.NoError(t, err)
		db := fixture.Open[VectorDB](t, target)
		require.NoError(
			t,
			db.Rows.Insert(
				ctx,
				&Embedded{Embedding: postgres.Vector{1, 0}},
				&Embedded{Embedding: postgres.Vector{0, 1}},
			),
		)

		bySimilarity := postgres.CosineDistance(&EM.Embedding, postgres.Vector{0.9, 0.1}).Asc()

		nearest, err := db.Rows.Order(bySimilarity).
			First(ctx)
		require.NoError(t, err)
		require.Equal(t, postgres.Vector{1, 0}, nearest.Embedding)
	})
}
