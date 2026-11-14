package benchmarks

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
)

// fake is a database/sql driver that answers every statement from memory, so
// that a benchmark on it measures only ant and database/sql: no network, no
// server, and the same numbers on every run. A SELECT returns rows many rows of
// the columns it names, and a statement with RETURNING returns one row for each
// row it wrote. Keys count up from fakeFirstID.
type fake struct {
	rows atomic.Int64
	// fan is how many rows a SELECT of a user_id column returns for each of
	// rows, so that each user has that many posts. Zero means one.
	fan atomic.Int64
	// log holds every statement run while recording is set, so that a
	// benchmark does not pay for it.
	recording atomic.Bool
	mu        sync.Mutex
	log       []fakeCall
}

// fakeCall is one statement the fake was given.
type fakeCall struct {
	SQL  string
	Args []any
}

// fakeFirstID is the first key of the rows the fake reads. It is past the small
// integers the runtime keeps ready, so a key costs the same at every size.
// fakeInsertedID is the first key it gives the rows of an INSERT, so that they
// never take the key of a row that was read.
const (
	fakeFirstID    = 1000
	fakeInsertedID = 1_000_000
)

func (f *fake) record(query string, args []driver.NamedValue) {
	if !f.recording.Load() {
		return
	}

	values := make([]any, len(args))

	for i, a := range args {
		values[i] = a.Value
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.log = append(f.log, fakeCall{query, values})
}

// record runs body and returns the statements it ran.
func (f *fake) recorded(body func()) []fakeCall {
	f.mu.Lock()
	f.log = nil
	f.mu.Unlock()

	f.recording.Store(true)
	defer f.recording.Store(false)

	body()

	f.mu.Lock()
	defer f.mu.Unlock()

	return f.log
}

var fakeTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func (f *fake) Connect(context.Context) (driver.Conn, error) { return &fakeConn{fake: f}, nil }

func (f *fake) Driver() driver.Driver { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return nil, driver.ErrBadConn }

type fakeConn struct {
	fake *fake
	// shapes remembers the columns of each statement, as a driver's statement
	// cache does.
	shapes map[string]fakeShape
}

type fakeShape struct {
	columns []string
	perRow  int // bound values per written row, or 0 for a SELECT
}

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }

func (c *fakeConn) Close() error { return nil }

func (c *fakeConn) Begin() (driver.Tx, error) { return fakeTx{}, nil }

func (c *fakeConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return fakeTx{}, nil
}

type fakeTx struct{}

func (fakeTx) Commit() error { return nil }

func (fakeTx) Rollback() error { return nil }

func (c *fakeConn) ExecContext(
	_ context.Context,
	query string,
	args []driver.NamedValue,
) (driver.Result, error) {
	c.fake.record(query, args)

	return driver.RowsAffected(1), nil
}

func (c *fakeConn) QueryContext(
	_ context.Context,
	query string,
	args []driver.NamedValue,
) (driver.Rows, error) {
	c.fake.record(query, args)

	shape, ok := c.shapes[query]
	if !ok {
		shape = shapeOf(query)

		if c.shapes == nil {
			c.shapes = map[string]fakeShape{}
		}

		c.shapes[query] = shape
	}

	left, fan := c.fake.rows.Load(), int64(1)

	switch {
	case shape.perRow > 0:
		left = int64(len(args) / shape.perRow)
	case slices.Contains(shape.columns, "user_id"), slices.Contains(shape.columns, "author_id"):
		fan = max(c.fake.fan.Load(), 1)
		left *= fan
	}

	first := int64(fakeFirstID)

	if shape.perRow > 0 {
		first = fakeInsertedID
	}

	return &fakeRows{
		columns: shape.columns,
		left:    left,
		fan:     fan,
		first:   first,
	}, nil
}

// shapeOf reads the result columns out of a statement.
func shapeOf(query string) fakeShape {
	var (
		list  string
		shape fakeShape
	)

	if _, after, ok := strings.Cut(query, " RETURNING "); ok {
		list = after

		if _, values, ok := strings.Cut(query, " VALUES ("); ok {
			row, _, _ := strings.Cut(values, ")")
			shape.perRow = strings.Count(row, "$")
		}

		shape.perRow = max(shape.perRow, 1)
	} else {
		list = strings.TrimPrefix(query, "SELECT ")
		list, _, _ = strings.Cut(list, " FROM ")
	}

	for name := range strings.SplitSeq(list, ",") {
		name = strings.TrimSpace(name)
		name = name[strings.LastIndexByte(name, '.')+1:]
		shape.columns = append(shape.columns, strings.Trim(name, `"`))
	}

	return shape
}

type fakeRows struct {
	columns []string
	left    int64
	n       int64
	fan     int64
	first   int64
}

func (r *fakeRows) Columns() []string { return r.columns }

func (r *fakeRows) Close() error { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.left == 0 {
		return io.EOF
	}

	r.left--
	r.n++

	for i, name := range r.columns {
		switch name {
		case "id":
			dest[i] = r.first + r.n - 1
		case "user_id", "author_id":
			dest[i] = fakeFirstID + (r.n-1)/r.fan
		case "age":
			dest[i] = int64(30)
		case "created_at", "updated_at":
			dest[i] = fakeTime
		case "bio":
			dest[i] = nil
		default:
			dest[i] = "some text"
		}
	}

	return nil
}

// fakeDB returns ant on the fake driver, and the pool under it.
func fakeDB(t testing.TB, options ...ant.Option) (*AppDB, *sql.DB, *fake) {
	t.Helper()

	f := &fake{}
	pool := sql.OpenDB(f)

	t.Cleanup(func() { pool.Close() })

	db, err := ant.Wrap[AppDB](postgres.Dialect, pool, options...)
	require.NoError(t, err)

	return db, pool, f
}

// The benchmarks below read and write through the fake driver, at one row, a
// hundred, and a hundred thousand, so that fixed costs per query and costs per
// row both show. database-sql is the same work written by hand, the floor that
// ant's overhead is measured against.

var fakeSizes = []int{1, 100, 100_000}

const fakeSelect = `SELECT id, created_at, updated_at, name, email, age, bio FROM users`

func BenchmarkFakeRead(b *testing.B) {
	db, raw, f := fakeDB(b, ant.WithTracking(false))
	ctx := context.Background()

	tracked, err := ant.Wrap[AppDB](postgres.Dialect, raw)
	require.NoError(b, err)

	for _, size := range fakeSizes {
		name := "rows=" + strconv.Itoa(size)

		b.Run(name+"/database-sql", func(b *testing.B) {
			f.rows.Store(int64(size))
			b.ReportAllocs()

			for b.Loop() {
				rows, err := raw.QueryContext(ctx, fakeSelect)
				if err != nil {
					b.Fatal(err)
				}

				var out []*User

				for rows.Next() {
					u := new(User)
					if err := rows.Scan(
						&u.ID,
						&u.CreatedAt,
						&u.UpdatedAt,
						&u.Name,
						&u.Email,
						&u.Age,
						&u.Bio,
					); err != nil {
						b.Fatal(err)
					}

					out = append(out, u)
				}

				rows.Close()

				if len(out) != size {
					b.Fatal(len(out))
				}
			}
		})

		b.Run(name+"/ant", func(b *testing.B) {
			f.rows.Store(int64(size))
			b.ReportAllocs()

			for b.Loop() {
				out, err := db.Users.All(ctx)
				if err != nil || len(out) != size {
					b.Fatal(err, len(out))
				}
			}
		})

		b.Run(name+"/ant-tracked", func(b *testing.B) {
			f.rows.Store(int64(size))
			b.ReportAllocs()

			for b.Loop() {
				session := ant.Session(tracked)

				out, err := session.Users.All(ctx)
				if err != nil || len(out) != size {
					b.Fatal(err, len(out))
				}
			}
		})

		b.Run(name+"/ant-each", func(b *testing.B) {
			f.rows.Store(int64(size))
			b.ReportAllocs()

			for b.Loop() {
				n := 0

				for _, err := range db.Users.Each(ctx) {
					if err != nil {
						b.Fatal(err)
					}

					n++
				}

				if n != size {
					b.Fatal(n)
				}
			}
		})
	}
}

func BenchmarkFakeFindOne(b *testing.B) {
	db, raw, f := fakeDB(b, ant.WithTracking(false))
	ctx := context.Background()

	f.rows.Store(1)

	b.Run("database-sql", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var u User

			err := raw.QueryRowContext(ctx, fakeSelect+` WHERE id = $1`, 500).
				Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.Name, &u.Email, &u.Age, &u.Bio)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ant", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if _, err := db.Users.Find(ctx, int64(500)); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ant-where", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if _, err := db.Users.Where(ant.Eq(&Users.Age, 30), ant.Like(&Users.Name, "user%")).
				OrderBy(&Users.Name).
				First(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ant-tracked-session", func(b *testing.B) {
		tracked, err := ant.Wrap[AppDB](postgres.Dialect, raw)
		require.NoError(b, err)

		b.ReportAllocs()

		for b.Loop() {
			session := ant.Session(tracked)

			if _, err := session.Users.Where(ant.Eq(&Users.Age, 30)).First(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkFakeInclude(b *testing.B) {
	db, _, f := fakeDB(b, ant.WithTracking(false))
	ctx := context.Background()

	for _, size := range []int{1, 100, 10_000} {
		b.Run("rows="+strconv.Itoa(size), func(b *testing.B) {
			f.rows.Store(int64(size))
			b.ReportAllocs()

			for b.Loop() {
				out, err := db.Users.Include(&Users.Posts).All(ctx)
				if err != nil || len(out) != size || len(out[0].Posts) != 1 {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkFakeInsert(b *testing.B) {
	db, raw, _ := fakeDB(b, ant.WithTracking(false))
	ctx := context.Background()

	b.Run("database-sql", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var id int64

			err := raw.QueryRowContext(
				ctx,
				`INSERT INTO users (created_at, updated_at, name, email, age) VALUES (now(), now(), $1, $2, $3) RETURNING id`,
				"x",
				"x",
				1,
			).Scan(&id)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ant", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if err := db.Users.Insert(ctx, &User{Name: "x", Email: "x", Age: 1}); err != nil {
				b.Fatal(err)
			}
		}
	})

	for _, size := range []int{100, 10_000} {
		users := make([]*User, size)

		b.Run("ant-many-"+strconv.Itoa(size), func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				for i := range users {
					users[i] = &User{Name: "x", Email: "x", Age: 1}
				}

				if _, err := db.Users.InsertMany(ctx, users); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkFakeSaveChanges(b *testing.B) {
	db, _, f := fakeDB(b)
	ctx := context.Background()

	for _, size := range []int{1, 100, 100_000} {
		f.rows.Store(int64(size))

		session := ant.Session(db)

		users, err := session.Users.All(ctx)
		require.NoError(b, err)

		b.Run("tracked="+strconv.Itoa(size)+"/nothing-changed", func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				if err := session.SaveChanges(ctx); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run("tracked="+strconv.Itoa(size)+"/one-changed", func(b *testing.B) {
			b.ReportAllocs()

			i := 0

			for b.Loop() {
				i++
				users[0].Age = i

				if err := session.SaveChanges(ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkFakeRetained reports the heap that stays in use while the results of
// one read are held, per row: the memory footprint of the rows, which B/op, a
// count of everything allocated along the way, does not show.
func BenchmarkFakeRetained(b *testing.B) {
	db, raw, f := fakeDB(b, ant.WithTracking(false))
	ctx := context.Background()

	tracked, err := ant.Wrap[AppDB](postgres.Dialect, raw)
	require.NoError(b, err)

	const size = 100_000

	f.rows.Store(size)

	measure := func(b *testing.B, read func() any) {
		var before, after runtime.MemStats

		for b.Loop() {
			runtime.GC()
			runtime.ReadMemStats(&before)

			held := read()

			runtime.GC()
			runtime.ReadMemStats(&after)
			runtime.KeepAlive(held)
		}

		b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc)/size, "retained-B/row")
	}

	b.Run("ant", func(b *testing.B) {
		measure(b, func() any {
			out, err := db.Users.All(ctx)
			if err != nil {
				b.Fatal(err)
			}

			return out
		})
	})

	b.Run("ant-tracked", func(b *testing.B) {
		measure(b, func() any {
			session := ant.Session(tracked)

			out, err := session.Users.All(ctx)
			if err != nil {
				b.Fatal(err)
			}

			return []any{session, out}
		})
	})
}
