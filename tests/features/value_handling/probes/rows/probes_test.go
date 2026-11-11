package rows

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"reflect"
	"runtime/debug"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/ids"
	"github.com/skudra-go/ant/secure"
	"github.com/skudra-go/ant/tests/features/fixture"
	"github.com/skudra-go/ant/tests/features/value_handling/probekit"
)

// Named types, as the named package probes them.
type (
	prStatus string
	prLevel  int16
)

// ---------------------------------------------------------------------------
// strings PostgreSQL cannot hold: an error, and no partial writes
// ---------------------------------------------------------------------------

func TestProbeBadStrings(t *testing.T) {
	probekit.KnownHere(t)

	ddl := `CREATE TABLE ` + probekit.PrTable[string](
		"row",
	) + ` (id BIGSERIAL PRIMARY KEY, v TEXT NOT NULL)`

	bad := map[string]string{
		"NUL byte":             "a\x00b",
		"invalid UTF-8":        "ab\xff\xfecd",
		"lone surrogate bytes": "\xed\xa0\x80",
	}

	for name, value := range bad {
		t.Run(name, func(t *testing.T) {
			probekit.KnownHere(t)

			fixture.RunWith(t, ddl, func(t *testing.T, db *probekit.PrDB[string]) {
				ctx := t.Context()
				f := ant.Fields[probekit.PrRow[string]]()

				count := func() int64 {
					n, err := db.Rows.Count(ctx)
					require.NoError(t, err)

					return n
				}

				// Insert of a batch: nothing is written, and no key is left behind.
				a, b, c := &probekit.PrRow[string]{
					V: "ok",
				}, &probekit.PrRow[string]{
					V: value,
				}, &probekit.PrRow[string]{
					V: "ok2",
				}

				if err := db.Rows.Insert(ctx, a, b, c); err == nil {
					t.Errorf("Insert accepted %q", value)
				}

				if n := count(); n != 0 {
					t.Errorf("a failed multi-row Insert left %d rows", n)
				}

				if a.ID != 0 || c.ID != 0 {
					t.Errorf(
						"failed Insert left keys on rolled-back entities: a=%d c=%d",
						a.ID,
						c.ID,
					)
				}

				// InsertMany: the same.
				rows := []*probekit.PrRow[string]{{V: "1"}, {V: "2"}, {V: value}, {V: "4"}}
				if _, err := db.Rows.InsertMany(ctx, rows, ant.BatchSize(2)); err == nil {
					t.Errorf("InsertMany accepted %q", value)
				}

				if n := count(); n != 0 {
					t.Errorf("a failed InsertMany left %d rows", n)
				}

				for i, r := range rows {
					if r.ID != 0 {
						t.Errorf("failed InsertMany left key %d on row %d", r.ID, i)
					}
				}

				// Update and Upsert.
				ok := &probekit.PrRow[string]{V: "fine"}
				require.NoError(t, db.Rows.Insert(ctx, ok))

				if _, err := db.Rows.Where(ant.Eq(&f.ID, ok.ID)).
					Update(ctx, ant.Set(&f.V, value)); err == nil {
					t.Errorf("Update accepted %q", value)
				}

				if _, err := db.Rows.Upsert(
					ctx,
					&probekit.PrRow[string]{ID: ok.ID, V: value},
					ant.OnConflict(&f.ID),
				); err == nil {
					t.Errorf("Upsert accepted %q", value)
				}

				got, err := db.Rows.NoTracking().Find(ctx, ok.ID)
				require.NoError(t, err)
				require.Equal(t, "fine", got.V)

				// A condition with such a value is an error, or matches nothing: never a row.
				n, err := db.Rows.Where(ant.Eq(&f.V, value)).Count(ctx)
				if err == nil && n != 0 {
					t.Errorf("Eq(%q) matched %d rows", value, n)
				}
			})
		})
	}
}

// ---------------------------------------------------------------------------
// bulk size limits
// ---------------------------------------------------------------------------

func TestProbeBigBatches(t *testing.T) {
	probekit.KnownHere(t)

	if testing.Short() {
		t.Skip("inserts 140,000 rows")
	}

	ddl := `CREATE TABLE ` + probekit.PrTable[int64](
		"row",
	) + ` (id BIGSERIAL PRIMARY KEY, v BIGINT NOT NULL)`
	fixture.RunWith(t, ddl, func(t *testing.T, db *probekit.PrDB[int64]) {
		ctx := t.Context()
		f := ant.Fields[probekit.PrRow[int64]]()

		const total = 140_000

		rows := make([]*probekit.PrRow[int64], total)
		for i := range rows {
			rows[i] = &probekit.PrRow[int64]{V: int64(i)}
		}

		n, err := db.Rows.InsertMany(ctx, rows, ant.BatchSize(1<<30))
		require.NoError(t, err)
		require.EqualValues(t, total, n)

		seen := map[int64]bool{}

		for i, r := range rows {
			if r.ID == 0 || seen[r.ID] {
				t.Fatalf("row %d has key %d (zero or duplicate)", i, r.ID)
			}

			seen[r.ID] = true
		}

		sum, err := db.Rows.Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, total, sum)

		// Keys match their rows.
		for _, i := range []int{0, 1, 999, 1000, 65534, 65535, 65536, total - 1} {
			got, err := db.Rows.NoTracking().Find(ctx, rows[i].ID)
			require.NoError(t, err)
			require.EqualValues(t, i, got.V, "row %d got key %d", i, rows[i].ID)
		}

		t.Run("In with a huge list", func(t *testing.T) {
			probekit.KnownHere(t)

			vals := make([]int64, 70_000)
			for i := range vals {
				vals[i] = int64(i)
			}

			c, err := db.Rows.Where(ant.In(&f.V, vals...)).Count(ctx)
			if err != nil {
				t.Errorf("In with %d values: %v", len(vals), err)

				return
			}

			if c != 70_000 {
				t.Errorf("In with %d values matched %d rows", len(vals), c)
			}
		})
	})
}

// ---------------------------------------------------------------------------
// embedded structs with prefixes
// ---------------------------------------------------------------------------

type prAddress struct {
	Street string
	City   string
	Zip    *string
	Geo    sql.Null[float64]
	Seen   time.Time
	Tags   probekit.PrTags
}

type prCustomer struct {
	ID       int64
	Name     string    `ant:"unique"`
	Billing  prAddress `ant:"embedded,prefix=billing_"`
	Shipping prAddress `ant:"embedded,prefix=ship_"`
	Note     *string
}

type prCustomerDB struct {
	ant.Context
	Customers ant.Table[prCustomer]
}

const prCustomerDDL = `CREATE TABLE pr_customers (
	id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL UNIQUE,
	billing_street TEXT NOT NULL, billing_city TEXT NOT NULL, billing_zip TEXT,
	billing_geo DOUBLE PRECISION, billing_seen TIMESTAMPTZ NOT NULL, billing_tags JSONB,
	ship_street TEXT NOT NULL, ship_city TEXT NOT NULL, ship_zip TEXT,
	ship_geo DOUBLE PRECISION, ship_seen TIMESTAMPTZ NOT NULL, ship_tags JSONB,
	note TEXT)`

func TestProbeEmbeddedPrefixes(t *testing.T) {
	probekit.KnownHere(t)

	fixture.RunTrackedWith(t, prCustomerDDL, func(t *testing.T, db *prCustomerDB) {
		ctx := t.Context()
		f := ant.Fields[prCustomer]()
		zip := "12345"
		seen := time.Date(2024, 3, 4, 5, 6, 7, 8000, time.FixedZone("z", 7200))

		a := &prCustomer{
			Name: "a",
			Billing: prAddress{
				Street: "b st",
				City:   "X",
				Zip:    &zip,
				Geo:    sql.Null[float64]{V: 1.5, Valid: true},
				Seen:   seen,
				Tags:   probekit.PrTags{"t1"},
			},
			Shipping: prAddress{Street: "s st", City: "Y", Seen: seen},
		}

		b := &prCustomer{
			Name:    "b",
			Billing: prAddress{Street: "b2", City: "Y", Seen: seen},
			Shipping: prAddress{
				Street: "s2",
				City:   "X",
				Zip:    &zip,
				Seen:   seen,
				Tags:   probekit.PrTags{},
			},
		}
		require.NoError(t, db.Customers.Insert(ctx, a, b))

		check := func(t *testing.T, want, got *prCustomer) {
			t.Helper()

			if want.Name != got.Name ||
				want.Billing.Street != got.Billing.Street || want.Billing.City != got.Billing.City ||
				want.Shipping.Street != got.Shipping.Street || want.Shipping.City != got.Shipping.City {
				t.Errorf(
					"columns mixed up: want %+v / %+v, got %+v / %+v",
					want.Billing,
					want.Shipping,
					got.Billing,
					got.Shipping,
				)
			}

			if (want.Billing.Zip == nil) != (got.Billing.Zip == nil) ||
				(want.Shipping.Zip == nil) != (got.Shipping.Zip == nil) {
				t.Errorf(
					"zip nil-ness: want %v/%v got %v/%v",
					want.Billing.Zip,
					want.Shipping.Zip,
					got.Billing.Zip,
					got.Shipping.Zip,
				)
			}

			if want.Billing.Geo != got.Billing.Geo || want.Shipping.Geo != got.Shipping.Geo {
				t.Errorf(
					"geo: want %v/%v got %v/%v",
					want.Billing.Geo,
					want.Shipping.Geo,
					got.Billing.Geo,
					got.Shipping.Geo,
				)
			}

			if !want.Billing.Seen.Equal(got.Billing.Seen) ||
				!want.Shipping.Seen.Equal(got.Shipping.Seen) {
				t.Errorf(
					"seen: want %v/%v got %v/%v",
					want.Billing.Seen,
					want.Shipping.Seen,
					got.Billing.Seen,
					got.Shipping.Seen,
				)
			}

			if !reflect.DeepEqual(want.Billing.Tags, got.Billing.Tags) ||
				!reflect.DeepEqual(want.Shipping.Tags, got.Shipping.Tags) {
				t.Errorf(
					"tags: want %#v/%#v got %#v/%#v",
					want.Billing.Tags,
					want.Shipping.Tags,
					got.Billing.Tags,
					got.Shipping.Tags,
				)
			}
		}

		got, err := db.Customers.NoTracking().Find(ctx, a.ID)
		require.NoError(t, err)
		check(t, a, got)

		got, err = db.Customers.NoTracking().Find(ctx, b.ID)
		require.NoError(t, err)
		check(t, b, got)

		count := func(c ...ant.Cond) int64 {
			n, err := db.Customers.Where(c...).Count(ctx)
			require.NoError(t, err)

			return n
		}

		for name, tc := range map[string]struct {
			cond ant.Cond
			want int64
		}{
			"Billing.City=X":     {ant.Eq(&f.Billing.City, "X"), 1},
			"Shipping.City=X":    {ant.Eq(&f.Shipping.City, "X"), 1},
			"City in both":       {ant.In(&f.Billing.City, "X", "Y"), 2},
			"Billing.Zip null":   {ant.IsNull(&f.Billing.Zip), 1},
			"Shipping.Zip null":  {ant.IsNull(&f.Shipping.Zip), 1},
			"Billing.Zip=&zip":   {ant.Eq(&f.Billing.Zip, &zip), 1},
			"Shipping.Zip=&zip":  {ant.Eq(&f.Shipping.Zip, &zip), 1},
			"Billing.Geo valid":  {ant.Eq(&f.Billing.Geo, sql.Null[float64]{V: 1.5, Valid: true}), 1},
			"Shipping.Geo null":  {ant.Eq(&f.Shipping.Geo, sql.Null[float64]{}), 2},
			"Billing.Seen":       {ant.Eq(&f.Billing.Seen, seen.UTC()), 2},
			"Billing.Seen in":    {ant.In(&f.Billing.Seen, seen), 2},
			"Billing.Seen gt":    {ant.Gt(&f.Billing.Seen, seen.Add(-time.Second)), 2},
			"Billing.Seen gt no": {ant.Gt(&f.Billing.Seen, seen.Add(time.Second)), 0},
		} {
			if n := count(tc.cond); n != tc.want {
				t.Errorf("%s: %d rows, want %d", name, n, tc.want)
			}
		}

		// Bulk update of one prefix leaves the other alone.
		n, err := db.Customers.Where(ant.Eq(&f.ID, a.ID)).
			Update(ctx, ant.Set(&f.Billing.City, "Z"), ant.Set(&f.Shipping.Zip, &zip), ant.Set(&f.Billing.Zip, nil))
		require.NoError(t, err)
		require.EqualValues(t, 1, n)

		got, err = db.Customers.NoTracking().Find(ctx, a.ID)
		require.NoError(t, err)
		require.Equal(t, "Z", got.Billing.City)
		require.Equal(t, "Y", got.Shipping.City)
		require.Nil(t, got.Billing.Zip)
		require.NotNil(t, got.Shipping.Zip)

		// Change tracking sees a change in one nested field only.
		require.NoError(t, db.ClearTracked(ctx))
		tracked, err := db.Customers.Find(ctx, b.ID)
		require.NoError(t, err)
		require.False(t, fixture.HasChanges(t, db))

		tracked.Shipping.Tags = append(tracked.Shipping.Tags, "added")
		tracked.Billing.Zip = &zip

		require.True(t, fixture.HasChanges(t, db))
		require.NoError(t, db.SaveChanges(ctx))

		got, err = db.Customers.NoTracking().Find(ctx, b.ID)
		require.NoError(t, err)
		require.Equal(t, probekit.PrTags{"added"}, got.Shipping.Tags)
		require.Equal(t, &zip, got.Billing.Zip)
		require.Equal(t, "Y", got.Billing.City)

		// Upsert by the unique name.
		up := &prCustomer{
			Name:    "a",
			Billing: prAddress{Street: "up", City: "U", Seen: seen},
			Shipping: prAddress{
				Street: "up2",
				City:   "V",
				Zip:    &zip,
				Seen:   seen,
			},
		}
		want := *up
		ok, err := db.Customers.Upsert(ctx, up, ant.OnConflict(&f.Name))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, a.ID, up.ID, "Upsert returns the stored key")
		check(t, &want, up)

		got, err = db.Customers.NoTracking().Find(ctx, a.ID)
		require.NoError(t, err)
		check(t, &want, got)

		// InsertMany.
		many := []*prCustomer{
			{
				Name:    "m1",
				Billing: prAddress{Street: "1", City: "1", Seen: seen},
				Shipping: prAddress{
					Street: "1",
					City:   "1",
					Seen:   seen,
					Zip:    &zip,
				},
			},
			{
				Name: "m2",
				Billing: prAddress{
					Street: "2",
					City:   "2",
					Seen:   seen,
					Zip:    &zip,
					Tags:   probekit.PrTags{"x"},
				},
				Shipping: prAddress{Street: "2", City: "2", Seen: seen},
			},
		}
		_, err = db.Customers.InsertMany(ctx, many)
		require.NoError(t, err)

		for _, m := range many {
			got, err = db.Customers.NoTracking().Find(ctx, m.ID)
			require.NoError(t, err)
			check(t, m, got)
		}
	})
}

// An embedded pointer to a struct: either a clear error or support, never a panic.
type prPtrEmbedded struct {
	ID   int64
	Addr *prAddress `ant:"embedded,prefix=addr_"`
}

type prPtrEmbeddedDB struct {
	ant.Context
	Rows ant.Table[prPtrEmbedded]
}

func TestProbeEmbeddedPointer(t *testing.T) {
	probekit.KnownHere(t)

	fixture.Each(t, func(t *testing.T, target fixture.Target) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("PANIC: %v\n%s", r, debug.Stack())
			}
		}()

		// A pointer is refused, with an error that names the field (#51).
		db, err := ant.Open[prPtrEmbeddedDB](t.Context(), target.Dialect, target.DSN)
		if err == nil {
			db.Close()
		}

		require.ErrorContains(t, err, "must be a struct value")
	})
}

// ---------------------------------------------------------------------------
// keys: uuid strings, uuid.UUID, big unsigned, small, negative
// ---------------------------------------------------------------------------

type prUUIDKey struct {
	ID   string
	Name string `ant:"unique"`
	Pad  *string
}

func (*prUUIDKey) Configure(b *ant.ModelBuilder[prUUIDKey]) {
	k := ant.Fields[prUUIDKey]()
	ant.GenerateKey(b, &k.ID, ids.UUID)
}

type prGoogleKey struct {
	ID   uuid.UUID
	Name string `ant:"unique"`
	Note string
}

func (*prGoogleKey) Configure(b *ant.ModelBuilder[prGoogleKey]) {
	k := ant.Fields[prGoogleKey]()
	ant.GenerateKey(b, &k.ID, uuid.New)
}

type prBigKey struct {
	ID   uint64
	Name string
}

type prSmallKey struct {
	ID   int32
	Name string
}

type prNamedKey struct {
	ID   prLevel
	Name string
}

type prKeysDB struct {
	ant.Context
	UUIDs   ant.Table[prUUIDKey]
	Googles ant.Table[prGoogleKey]
	Bigs    ant.Table[prBigKey]
	Smalls  ant.Table[prSmallKey]
	Named   ant.Table[prNamedKey]
}

const prKeysDDL = `
	CREATE TABLE pr_uuid_keys (id UUID PRIMARY KEY, name TEXT NOT NULL UNIQUE, pad TEXT);
	CREATE TABLE pr_google_keys (id UUID PRIMARY KEY, name TEXT NOT NULL UNIQUE, note TEXT NOT NULL DEFAULT '');
	CREATE TABLE pr_big_keys (id NUMERIC(20,0) PRIMARY KEY, name TEXT NOT NULL);
	CREATE TABLE pr_small_keys (id SERIAL PRIMARY KEY, name TEXT NOT NULL);
	CREATE TABLE pr_named_keys (id SMALLSERIAL PRIMARY KEY, name TEXT NOT NULL)`

func TestProbeKeys(t *testing.T) {
	probekit.KnownHere(t)

	fixture.RunTrackedWith(t, prKeysDDL, func(t *testing.T, db *prKeysDB) {
		ctx := t.Context()

		t.Run("string uuid generated", func(t *testing.T) {
			probekit.KnownHere(t)

			k := ant.Fields[prUUIDKey]()
			a, b := &prUUIDKey{Name: "a"}, &prUUIDKey{Name: "b"}
			require.NoError(t, db.UUIDs.Insert(ctx, a, b))
			require.NotEmpty(t, a.ID)
			require.NotEqual(t, a.ID, b.ID)

			got, err := db.UUIDs.NoTracking().Find(ctx, a.ID)
			require.NoError(t, err)
			require.Equal(t, "a", got.Name)

			many := []*prUUIDKey{
				{Name: "m1"},
				{Name: "m2"},
				{ID: "00000000-0000-4000-8000-000000000001", Name: "m3"},
			}
			_, err = db.UUIDs.InsertMany(ctx, many)
			require.NoError(t, err)

			for _, m := range many {
				require.NotEmpty(t, m.ID, "InsertMany did not generate a key for %s", m.Name)
			}

			require.Equal(t, "00000000-0000-4000-8000-000000000001", many[2].ID)

			n, err := db.UUIDs.Where(ant.In(&k.ID, a.ID, b.ID, many[0].ID)).Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 3, n)

			// Upsert by name with no key: the stored key comes back.
			up := &prUUIDKey{Name: "a", Pad: new("padded")}
			ok, err := db.UUIDs.Upsert(ctx, up, ant.OnConflict(&k.Name))
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, a.ID, up.ID, "Upsert returns the stored row's key")

			total, err := db.UUIDs.Where(ant.Eq(&k.Name, "a")).Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, total)

			// InsertMany with a conflict on name.
			again := []*prUUIDKey{{Name: "a", Pad: new("again")}, {Name: "fresh"}}
			_, err = db.UUIDs.InsertMany(ctx, again, ant.WithConflict(ant.OnConflict(&k.Name)))
			require.NoError(t, err)

			got, err = db.UUIDs.NoTracking().Find(ctx, a.ID)
			require.NoError(t, err)
			require.Equal(t, "again", *got.Pad)

			require.NoError(t, db.UUIDs.DeleteByID(ctx, a.ID))
		})

		t.Run("uuid.UUID generated", func(t *testing.T) {
			probekit.KnownHere(t)

			k := ant.Fields[prGoogleKey]()
			a := &prGoogleKey{Name: "a"}
			require.NoError(t, db.Googles.Insert(ctx, a))
			require.NotEqual(t, uuid.Nil, a.ID)

			got, err := db.Googles.NoTracking().Find(ctx, a.ID)
			require.NoError(t, err)
			require.Equal(t, a.ID, got.ID)

			n, err := db.Googles.Where(ant.Eq(&k.ID, a.ID)).Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)

			n, err = db.Googles.Where(ant.In(&k.ID, a.ID, uuid.Nil)).Count(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)

			up := &prGoogleKey{Name: "a"}
			ok, err := db.Googles.Upsert(ctx, up, ant.OnConflict(&k.Name))
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, a.ID, up.ID)

			many := []*prGoogleKey{{Name: "m1"}, {Name: "m2"}}
			_, err = db.Googles.InsertMany(ctx, many)
			require.NoError(t, err)
			require.NotEqual(t, many[0].ID, many[1].ID)
			require.NotEqual(t, uuid.Nil, many[0].ID)
		})

		t.Run("uint64 key above int64", func(t *testing.T) {
			probekit.KnownHere(t)

			big := &prBigKey{ID: math.MaxUint64, Name: "max"}
			require.NoError(t, db.Bigs.Insert(ctx, big))

			got, err := db.Bigs.NoTracking().Find(ctx, uint64(math.MaxUint64))
			require.NoError(t, err)
			require.Equal(t, "max", got.Name)

			ok, err := db.Bigs.Upsert(
				ctx,
				&prBigKey{ID: math.MaxUint64, Name: "again"},
				ant.OnConflict(&ant.Fields[prBigKey]().ID),
			)
			require.NoError(t, err)
			require.True(t, ok)

			got, err = db.Bigs.NoTracking().Find(ctx, uint64(math.MaxUint64))
			require.NoError(t, err)
			require.Equal(t, "again", got.Name)

			gen := &prBigKey{Name: "generated"}
			err = db.Bigs.Insert(ctx, gen)
			t.Logf("zero uint64 key on a NUMERIC column with no default: err=%v id=%d", err, gen.ID)
		})

		t.Run("int32 and named int16 generated keys", func(t *testing.T) {
			probekit.KnownHere(t)

			s := &prSmallKey{Name: "s"}
			require.NoError(t, db.Smalls.Insert(ctx, s))
			require.NotZero(t, s.ID)

			many := []*prSmallKey{{Name: "a"}, {Name: "b"}, {Name: "c"}}
			_, err := db.Smalls.InsertMany(ctx, many)
			require.NoError(t, err)
			require.Less(t, many[0].ID, many[1].ID)
			require.Less(t, many[1].ID, many[2].ID)

			n := &prNamedKey{Name: "n"}
			require.NoError(t, db.Named.Insert(ctx, n))
			require.NotZero(t, n.ID)

			got, err := db.Named.NoTracking().Find(ctx, n.ID)
			require.NoError(t, err)
			require.Equal(t, "n", got.Name)

			nm := []*prNamedKey{{Name: "x"}, {Name: "y"}}
			_, err = db.Named.InsertMany(ctx, nm)
			require.NoError(t, err)
			require.NotZero(t, nm[0].ID)
			require.NotEqual(t, nm[0].ID, nm[1].ID)
		})
	})
}

// Extreme explicit keys.
func TestProbeExtremeKeys(t *testing.T) {
	probekit.KnownHere(t)

	ddl := `CREATE TABLE ` + probekit.PrTable[string](
		"row",
	) + ` (id BIGINT PRIMARY KEY, v TEXT NOT NULL)`
	fixture.RunTrackedWith(t, ddl, func(t *testing.T, db *probekit.PrDB[string]) {
		ctx := t.Context()

		for _, id := range []int64{math.MinInt64, -1, 1, math.MaxInt64} {
			e := &probekit.PrRow[string]{ID: id, V: fmt.Sprint(id)}
			require.NoError(t, db.Rows.Insert(ctx, e), "key %d", id)

			got, err := db.Rows.Find(ctx, id)
			require.NoError(t, err)
			require.Equal(t, fmt.Sprint(id), got.V)

			got.V += "!"

			require.NoError(t, db.SaveChanges(ctx), "SaveChanges key %d", id)

			again, err := db.Rows.NoTracking().Find(ctx, id)
			require.NoError(t, err)
			require.Equal(t, fmt.Sprint(id)+"!", again.V)
			require.NoError(t, db.Rows.DeleteByID(ctx, id))
		}
	})
}

// ---------------------------------------------------------------------------
// zero values, defaults, and timestamps
// ---------------------------------------------------------------------------

type prDefaulted struct {
	ID     int64
	Name   string
	Active bool      `ant:"default=true"`
	Score  int       `ant:"default=7"`
	Label  *string   `ant:"default='dflt'"`
	Seen   time.Time `ant:"default=now()"`
}

type prDefaultedDB struct {
	ant.Context
	Rows ant.Table[prDefaulted]
}

const prDefaultedDDL = `CREATE TABLE pr_defaulteds (
	id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL UNIQUE,
	active BOOLEAN NOT NULL DEFAULT TRUE, score INT NOT NULL DEFAULT 7,
	label TEXT DEFAULT 'dflt', seen TIMESTAMPTZ NOT NULL DEFAULT now())`

// Documented: a zero field with a default is left for the database. The three
// ways to insert must agree, and SaveChanges / Set must still write zero.
func TestProbeDefaults(t *testing.T) {
	probekit.KnownHere(t)

	fixture.RunTrackedWith(t, prDefaultedDDL, func(t *testing.T, db *prDefaultedDB) {
		ctx := t.Context()
		f := ant.Fields[prDefaulted]()
		empty := ""

		read := func(name string) *prDefaulted {
			got, err := db.Rows.NoTracking().Where(ant.Eq(&f.Name, name)).Single(ctx)
			require.NoError(t, err)

			return got
		}

		one := &prDefaulted{Name: "one"}
		require.NoError(t, db.Rows.Insert(ctx, one))

		if !one.Active || one.Score != 7 || one.Label == nil || *one.Label != "dflt" ||
			one.Seen.IsZero() {
			t.Errorf("Insert did not copy the defaults back: %+v", one)
		}

		many := []*prDefaulted{{Name: "many"}, {Name: "many2", Score: 3, Label: &empty}}
		_, err := db.Rows.InsertMany(ctx, many)
		require.NoError(t, err)

		got := read("many")
		if !got.Active || got.Score != 7 || got.Label == nil || *got.Label != "dflt" {
			t.Errorf("InsertMany zero fields: %+v (want the defaults, as Insert does)", got)
		}

		got = read("many2")
		if got.Score != 3 || got.Label == nil || *got.Label != "" {
			t.Errorf("InsertMany explicit fields: %+v", got)
		}

		up := &prDefaulted{Name: "up"}
		ok, err := db.Rows.Upsert(ctx, up, ant.OnConflict(&f.Name))
		require.NoError(t, err)
		require.True(t, ok)

		if !up.Active || up.Score != 7 || up.Label == nil {
			t.Errorf("Upsert did not copy the defaults back: %+v", up)
		}

		// Zero values written by SaveChanges and Set are real zeros.
		tracked, err := db.Rows.Find(ctx, one.ID)
		require.NoError(t, err)

		tracked.Active, tracked.Score, tracked.Label = false, 0, nil

		require.NoError(t, db.SaveChanges(ctx))

		got = read("one")
		if got.Active || got.Score != 0 || got.Label != nil {
			t.Errorf("SaveChanges of zero values: %+v", got)
		}

		_, err = db.Rows.Where(ant.Eq(&f.Name, "many")).
			Update(ctx, ant.Set(&f.Active, false), ant.Set(&f.Score, 0), ant.Set(&f.Label, (*string)(nil)))
		require.NoError(t, err)

		got = read("many")
		if got.Active || got.Score != 0 || got.Label != nil {
			t.Errorf("bulk Set of zero values: %+v", got)
		}

		// Upserting the zero value over an existing row with explicit values.
		over := &prDefaulted{Name: "many2"}
		ok, err = db.Rows.Upsert(ctx, over, ant.OnConflict(&f.Name))
		require.NoError(t, err)
		require.True(t, ok)

		t.Logf(
			"Upsert{Name only} over {Score:3, Label:\"\"}: score=%d label=%v (zero = left for the database => default 7/'dflt')",
			over.Score,
			over.Label,
		)
	})
}

// Explicit CreatedAt / UpdatedAt are kept; zero ones are stamped; times survive.
type prStamped struct {
	ID        int64
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type prStampedDB struct {
	ant.Context
	Rows ant.Table[prStamped]
}

func TestProbeTimestamps(t *testing.T) {
	probekit.KnownHere(t)

	const ddl = `CREATE TABLE pr_stampeds (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL UNIQUE,
		created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL)`

	fixture.RunTrackedWith(t, ddl, func(t *testing.T, db *prStampedDB) {
		ctx := t.Context()
		f := ant.Fields[prStamped]()
		past := time.Date(2001, 2, 3, 4, 5, 6, 0, time.FixedZone("p", -3*3600))

		explicit := &prStamped{Name: "explicit", CreatedAt: past, UpdatedAt: past}
		require.NoError(t, db.Rows.Insert(ctx, explicit))

		got, err := db.Rows.NoTracking().Find(ctx, explicit.ID)
		require.NoError(t, err)

		if !got.CreatedAt.Equal(past) {
			t.Errorf("explicit CreatedAt %v stored as %v", past, got.CreatedAt)
		}

		if !got.UpdatedAt.Equal(past) {
			t.Errorf("explicit UpdatedAt %v stored as %v", past, got.UpdatedAt)
		}

		stamped := &prStamped{Name: "stamped"}
		require.NoError(t, db.Rows.Insert(ctx, stamped))
		require.WithinDuration(t, time.Now(), stamped.CreatedAt, time.Minute)

		got, err = db.Rows.NoTracking().Find(ctx, stamped.ID)
		require.NoError(t, err)

		if !got.CreatedAt.Equal(stamped.CreatedAt.Truncate(time.Microsecond)) {
			t.Errorf("stamped CreatedAt %v read back as %v", stamped.CreatedAt, got.CreatedAt)
		}

		// A condition on the entity's own (nanosecond) timestamp finds its row.
		n, err := db.Rows.Where(ant.Eq(&f.CreatedAt, stamped.CreatedAt)).Count(ctx)
		require.NoError(t, err)

		if n != 1 {
			t.Errorf("Eq(CreatedAt, entity.CreatedAt) found %d rows, want 1", n)
		}

		// The same instant in another zone matches.
		n, err = db.Rows.Where(ant.Eq(&f.CreatedAt, past.UTC())).Count(ctx)
		require.NoError(t, err)

		if n != 1 {
			t.Errorf("Eq(same instant, UTC) found %d rows, want 1", n)
		}

		n, err = db.Rows.Where(ant.Between(&f.CreatedAt, past.Add(-time.Second), past.Add(time.Second))).
			Count(ctx)
		require.NoError(t, err)

		if n != 1 {
			t.Errorf("Between around %v found %d rows, want 1", past, n)
		}

		// A bulk update stamps UpdatedAt only.
		_, err = db.Rows.Where(ant.Eq(&f.ID, explicit.ID)).Update(ctx, ant.Set(&f.Name, "renamed"))
		require.NoError(t, err)

		got, err = db.Rows.NoTracking().Find(ctx, explicit.ID)
		require.NoError(t, err)

		if !got.CreatedAt.Equal(past) {
			t.Errorf("bulk Update changed CreatedAt to %v", got.CreatedAt)
		}

		if got.UpdatedAt.Equal(past) {
			t.Errorf("bulk Update did not touch UpdatedAt")
		}

		// A tracked change stamps UpdatedAt, and the entity shows it.
		tracked, err := db.Rows.Find(ctx, stamped.ID)
		require.NoError(t, err)

		before := tracked.UpdatedAt
		tracked.Name = "stamped2"

		require.NoError(t, db.SaveChanges(ctx))

		if !tracked.UpdatedAt.After(before) {
			t.Errorf("SaveChanges did not advance UpdatedAt: %v -> %v", before, tracked.UpdatedAt)
		}

		if fixture.HasChanges(t, db) {
			t.Errorf("HasChanges after SaveChanges: %s", fixture.DebugTracked(t, db))
		}

		// Upsert with explicit timestamps overwrites them (UpdatedAt) as written.
		ok, err := db.Rows.Upsert(
			ctx,
			&prStamped{Name: "stamped2", CreatedAt: past, UpdatedAt: past},
			ant.OnConflict(&f.Name),
		)
		require.NoError(t, err)
		require.True(t, ok)

		got, err = db.Rows.NoTracking().Find(ctx, stamped.ID)
		require.NoError(t, err)

		t.Logf(
			"Upsert with explicit past stamps over a row: created=%v updated=%v",
			got.CreatedAt,
			got.UpdatedAt,
		)
	})
}

// A condition and a write with the same time must agree whatever the zone.
func TestProbeTimeZones(t *testing.T) {
	probekit.KnownHere(t)

	ddl := `CREATE TABLE ` + probekit.PrTable[time.Time](
		"row",
	) + ` (id BIGSERIAL PRIMARY KEY, v TIMESTAMPTZ NOT NULL)`

	zones := []*time.Location{
		time.UTC,
		time.FixedZone("p", 14*3600),
		time.FixedZone("m", -12*3600),
		time.FixedZone("odd", 5*3600+45*60+30),
		time.Local,
	}

	if ny, err := time.LoadLocation("America/New_York"); err == nil {
		zones = append(zones, ny)
	}

	fixture.RunWith(t, ddl, func(t *testing.T, db *probekit.PrDB[time.Time]) {
		ctx := t.Context()
		f := ant.Fields[probekit.PrRow[time.Time]]()

		base := time.Date(
			2024,
			11,
			3,
			1,
			30,
			0,
			500_000_000,
			time.UTC,
		) // inside the NY fall-back hour

		e := &probekit.PrRow[time.Time]{V: base}
		require.NoError(t, db.Rows.Insert(ctx, e))

		for _, z := range zones {
			in := base.In(z)

			n, err := db.Rows.Where(ant.Eq(&f.V, in)).Count(ctx)
			require.NoError(t, err)

			if n != 1 {
				t.Errorf("Eq(%v) found %d rows, want 1", in, n)
			}

			n, err = db.Rows.Where(ant.In(&f.V, in)).Count(ctx)
			require.NoError(t, err)

			if n != 1 {
				t.Errorf("In(%v) found %d rows, want 1", in, n)
			}

			// A wall-clock time in the zone, built with time.Date, round-trips as an instant.
			wall := time.Date(2024, 11, 3, 1, 30, 0, 0, z)
			w := &probekit.PrRow[time.Time]{V: wall}
			require.NoError(t, db.Rows.Insert(ctx, w))

			got, err := db.Rows.NoTracking().Find(ctx, w.ID)
			require.NoError(t, err)

			if !got.V.Equal(wall) {
				t.Errorf("wall clock in %v: wrote %v, read %v", z, wall, got.V)
			}
		}
	})
}

// A Go time in the far past or future that PostgreSQL stores fine must come back.
func TestProbeTimeLimits(t *testing.T) {
	probekit.KnownHere(t)

	ddl := `CREATE TABLE ` + probekit.PrTable[time.Time](
		"row",
	) + ` (id BIGSERIAL PRIMARY KEY, v TIMESTAMPTZ NOT NULL)`

	fixture.RunWith(t, ddl, func(t *testing.T, db *probekit.PrDB[time.Time]) {
		ctx := t.Context()

		for _, v := range []time.Time{
			time.Date(-4000, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(-1, 12, 31, 23, 59, 59, 0, time.UTC),
			time.Date(1, 1, 1, 0, 0, 0, 1000, time.UTC),
			time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(200000, 6, 7, 8, 9, 10, 0, time.UTC),
			time.Unix(1<<40, 0).UTC(),
		} {
			e := &probekit.PrRow[time.Time]{V: v}
			if err := db.Rows.Insert(ctx, e); err != nil {
				t.Errorf("Insert %v: %v", v, err)

				continue
			}

			got, err := db.Rows.NoTracking().Find(ctx, e.ID)
			if err != nil {
				t.Errorf("Find %v: %v", v, err)

				continue
			}

			if !got.V.Equal(v) {
				t.Errorf("wrote %v, read %v", v, got.V)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// keys that are times, bytes, or composite with a time
// ---------------------------------------------------------------------------

type prTimeKey struct {
	ID   time.Time
	Name string
}

type prBytesKey struct {
	ID   []byte
	Name string
}

type prReading struct {
	DeviceID int64
	At       time.Time
	Value    float64
	Tag      sql.NullString
}

func (*prReading) Configure(b *ant.ModelBuilder[prReading]) {
	k := ant.Fields[prReading]()
	b.CompositeKey(ant.F(&k.DeviceID), ant.F(&k.At))
}

type prOddKeysDB struct {
	ant.Context
	TimeKeys ant.Table[prTimeKey]
	ByteKeys ant.Table[prBytesKey]
	Readings ant.Table[prReading]
}

const prOddKeysDDL = `
	CREATE TABLE pr_time_keys (id TIMESTAMPTZ PRIMARY KEY, name TEXT NOT NULL);
	CREATE TABLE pr_bytes_keys (id BYTEA PRIMARY KEY, name TEXT NOT NULL);
	CREATE TABLE pr_readings (device_id BIGINT NOT NULL, at TIMESTAMPTZ NOT NULL, value DOUBLE PRECISION NOT NULL,
		tag TEXT, PRIMARY KEY (device_id, at))`

func TestProbeOddKeys(t *testing.T) {
	probekit.KnownHere(t)

	fixture.RunTrackedWith(t, prOddKeysDDL, func(t *testing.T, db *prOddKeysDB) {
		ctx := t.Context()

		t.Run("time key: identity, Find, SaveChanges", func(t *testing.T) {
			probekit.KnownHere(t)

			at := time.Date(2024, 1, 2, 3, 4, 5, 123_456_000, time.UTC)
			e := &prTimeKey{ID: at, Name: "a"}
			require.NoError(t, db.TimeKeys.Insert(ctx, e))

			got, err := db.TimeKeys.Find(ctx, at.In(time.FixedZone("z", 3600)))
			require.NoError(t, err, "Find by the same instant in another zone")

			if got != e {
				t.Errorf("Find returned a second instance of the tracked row")
			}

			all, err := db.TimeKeys.OrderBy(&ant.Fields[prTimeKey]().Name).All(ctx)
			require.NoError(t, err)
			require.Len(t, all, 1)

			if all[0] != e {
				t.Errorf(
					"All returned a second instance of the tracked row (identity lost for a time key)",
				)
			}

			e.Name = "b"

			require.NoError(t, db.SaveChanges(ctx))

			fresh, err := db.TimeKeys.NoTracking().Find(ctx, at)
			require.NoError(t, err)
			require.Equal(t, "b", fresh.Name)

			require.NoError(t, db.ClearTracked(ctx))
			loaded, err := db.TimeKeys.Find(ctx, at)
			require.NoError(t, err)

			loaded.Name = "c"

			require.NoError(t, db.SaveChanges(ctx))

			fresh, err = db.TimeKeys.NoTracking().Find(ctx, at)
			require.NoError(t, err)
			require.Equal(t, "c", fresh.Name)
		})

		t.Run("[]byte key", func(t *testing.T) {
			probekit.KnownHere(t)

			e := &prBytesKey{ID: []byte{0, 1, 2, 255}, Name: "a"}
			require.NoError(t, db.ByteKeys.Insert(ctx, e))

			//antvet:ignore the probe asks what a string key does for a []byte key
			got, err := db.ByteKeys.Find(ctx, string([]byte{0, 1, 2, 255}))
			if err != nil {
				t.Logf("Find with a string for a []byte key: %v", err)
			} else if got != e {
				t.Errorf("Find returned a second instance of the tracked row")
			}

			e.Name = "b"

			require.NoError(t, db.SaveChanges(ctx))

			fresh, err := db.ByteKeys.NoTracking().
				Where(ant.SQLCond("id = ?", []byte{0, 1, 2, 255})).
				Single(ctx)
			require.NoError(t, err)
			require.Equal(t, "b", fresh.Name)
		})

		t.Run("composite key with a time", func(t *testing.T) {
			probekit.KnownHere(t)

			f := ant.Fields[prReading]()
			at := time.Now()
			r := &prReading{DeviceID: 7, At: at, Value: 1.5}
			require.NoError(t, db.Readings.Insert(ctx, r))
			require.False(t, fixture.HasChanges(t, db))

			all, err := db.Readings.All(ctx)
			require.NoError(t, err)
			require.Len(t, all, 1)

			if all[0] != r {
				t.Errorf(
					"All returned a second instance of the tracked row (identity lost for a composite key with a time)",
				)
			}

			got, err := db.Readings.FindKey(ctx, int64(7), at)
			require.NoError(t, err)

			if got != r {
				t.Errorf("FindKey returned a second instance of the tracked row")
			}

			r.Value = 2.5
			r.Tag = sql.NullString{String: "t", Valid: true}

			require.NoError(t, db.SaveChanges(ctx))

			fresh, err := db.Readings.NoTracking().Where(ant.Eq(&f.DeviceID, int64(7))).Single(ctx)
			require.NoError(t, err)
			require.Equal(t, 2.5, fresh.Value)
			require.Equal(t, sql.NullString{String: "t", Valid: true}, fresh.Tag)

			ok, err := db.Readings.Upsert(
				ctx,
				&prReading{DeviceID: 7, At: at, Value: 9},
				ant.OnConflict(&f.DeviceID).And(ant.F(&f.At)),
			)
			require.NoError(t, err)
			require.True(t, ok)

			n, err := db.Readings.Count(ctx)
			require.NoError(t, err)

			require.EqualValues(
				t,
				1,
				n,
				"Upsert on a composite key with a ns time made a second row",
			)

			// Delete by the tracked entity.
			require.NoError(t, ant.Remove(ctx, db, r))
			require.NoError(t, db.SaveChanges(ctx))

			n, err = db.Readings.Count(ctx)
			require.NoError(t, err)
			require.Zero(t, n, "Remove + SaveChanges of a composite-key row with a ns time")
		})
	})
}

// PostgreSQL prints non-finite floats as Infinity / -Infinity / NaN, and Point.Value
// writes +Inf / -Inf; both must scan back.
func TestProbePointScanFormats(t *testing.T) {
	probekit.KnownHere(t)

	for _, text := range []string{
		"(Infinity,-Infinity)", "(NaN,1)", "(1,2)", "(1e+21,2)", "(+Inf,1)", "(-Inf,1)",
	} {
		var p postgres.Point
		if err := p.Scan(text); err != nil {
			t.Errorf("Point.Scan(%q): %v", text, err)
		}
	}
}

// ---------------------------------------------------------------------------
// NULL read into a Go type that cannot hold NULL: an error or a documented zero,
// never a panic. Logged, not asserted, because the answer differs by type.
// ---------------------------------------------------------------------------

func nullInto[V any](t *testing.T, sqlType string) {
	t.Helper()

	var zero V

	t.Run(fmt.Sprintf("%T", zero), func(t *testing.T) {
		probekit.KnownHere(t)

		ddl := `CREATE TABLE ` + probekit.PrTable[V](
			"row",
		) + ` (id BIGSERIAL PRIMARY KEY, v ` + sqlType + `)`
		fixture.RunWith(t, ddl, func(t *testing.T, db *probekit.PrDB[V]) {
			ctx := t.Context()

			_, err := db.DB().
				ExecContext(ctx, "INSERT INTO "+probekit.PrTable[V]("row")+" (v) VALUES (NULL)")
			require.NoError(t, err)

			defer func() {
				if r := recover(); r != nil {
					t.Errorf("PANIC reading NULL: %v", r)
				}
			}()

			rows, err := db.Rows.NoTracking().All(ctx)
			if err != nil {
				t.Logf("NULL into %T: error: %v", zero, err)

				return
			}

			t.Logf("NULL into %T: silently %#v", zero, rows[0].V)
		})
	})
}

func TestProbeNullIntoNonNullable(t *testing.T) {
	probekit.KnownHere(t)

	nullInto[int64](t, "BIGINT")
	nullInto[string](t, "TEXT")
	nullInto[bool](t, "BOOLEAN")
	nullInto[float64](t, "DOUBLE PRECISION")
	nullInto[time.Time](t, "TIMESTAMPTZ")
	nullInto[[]byte](t, "BYTEA")
	nullInto[ant.UnixTime](t, "BIGINT")
	nullInto[ant.JSON[probekit.PrPair]](t, "JSONB")
	nullInto[ant.Gob[probekit.PrPair]](t, "BYTEA")
	nullInto[uuid.UUID](t, "UUID")
	nullInto[decimal.Decimal](t, "NUMERIC")
	nullInto[postgres.Point](t, "POINT")
	nullInto[postgres.Array[string]](t, "TEXT[]")
	nullInto[prStatus](t, "TEXT")
	nullInto[probekit.PrMoney](t, "NUMERIC")
	nullInto[probekit.PrOptional](t, "BIGINT")
	nullInto[secure.Value[string]](t, "TEXT")
	nullInto[time.Duration](t, "BIGINT")
	nullInto[probekit.PrTags](t, "JSONB")
	nullInto[json.RawMessage](t, "JSONB")
	nullInto[netip.Addr](t, "INET")
}

// The reverse: a value too big for the Go type is an error, not a wrapped number.
func TestProbeOutOfRangeReads(t *testing.T) {
	probekit.KnownHere(t)

	check := func(t *testing.T, name, sqlType, literal string, read func(ctx context.Context, h ant.Handle) error) {
		t.Run(name, func(t *testing.T) {
			probekit.KnownHere(t)

			fixture.RunWith(
				t,
				`CREATE TABLE pr_probe_tmp (v `+sqlType+`)`,
				func(t *testing.T, db *probekit.PrDB[int64]) {
					ctx := t.Context()
					_, err := db.DB().
						ExecContext(ctx, "INSERT INTO pr_probe_tmp VALUES ("+literal+")")
					require.NoError(t, err)

					if err := read(ctx, db); err == nil {
						t.Errorf("%s: no error reading %s", name, literal)
					}
				},
			)
		})
	}

	check(t, "300 into uint8", "INT", "300", func(ctx context.Context, h ant.Handle) error {
		_, err := ant.Raw[uint8](h, "SELECT v FROM pr_probe_tmp").First(ctx)

		return err
	})

	check(t, "-1 into uint64", "BIGINT", "-1", func(ctx context.Context, h ant.Handle) error {
		_, err := ant.Raw[uint64](h, "SELECT v FROM pr_probe_tmp").First(ctx)

		return err
	})

	check(
		t,
		"2^40 into int32",
		"BIGINT",
		"1099511627776",
		func(ctx context.Context, h ant.Handle) error {
			_, err := ant.Raw[int32](h, "SELECT v FROM pr_probe_tmp").First(ctx)

			return err
		},
	)

	check(t, "1.5 into int64", "NUMERIC", "1.5", func(ctx context.Context, h ant.Handle) error {
		_, err := ant.Raw[int64](h, "SELECT v FROM pr_probe_tmp").First(ctx)

		return err
	})

	check(
		t,
		"1e300 into float32",
		"DOUBLE PRECISION",
		"1e300",
		func(ctx context.Context, h ant.Handle) error {
			_, err := ant.Raw[float32](h, "SELECT v FROM pr_probe_tmp").First(ctx)

			return err
		},
	)
}

// ant.JSON also replaces invalid UTF-8, silently, as encoding/json does.
// encoding/json writes U+FFFD in place of text that is not valid UTF-8, so ant
// refuses such text, as a TEXT column does, rather than store something else.
func TestProbeJSONInvalidUTF8(t *testing.T) {
	probekit.RefusesInvalidUTF8(t, ant.JSON[string]{V: "ab\xffcd"}, "JSONB")
	probekit.RefusesInvalidUTF8(t, ant.JSON[map[string]any]{V: map[string]any{"k\xff": 1}}, "JSONB")

	probekit.RefusesInvalidUTF8(
		t,
		ant.JSON[map[string]any]{V: map[string]any{"k": []any{"ok", "x\xfe"}}},
		"JSONB",
	)
}
