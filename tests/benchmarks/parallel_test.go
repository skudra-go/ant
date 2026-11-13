package benchmarks

import (
	"context"
	"database/sql"
	"runtime"
	"testing"

	"github.com/skudra-go/ant"
)

// The benchmarks below run the same work from GOMAXPROCS goroutines at once, as a
// server does, so that contention on shared state shows up. Compare core counts
// with -cpu 1,4,12: ns/op is wall time per operation across all goroutines, so
// work that scales divides by the core count, and work that contends does not.

func BenchmarkParallelProbeResolution(b *testing.B) {
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = ant.F(&Users.Age)
		}
	})
}

func BenchmarkParallelBuildQuery(b *testing.B) {
	db := unconnected(b)
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			q := db.Users.Where(ant.Eq(&Users.Age, 18), ant.Like(&Users.Name, "A%")).
				OrderBy(&Users.Name).
				Limit(10)

			if _, _, err := q.ToSQL(); err != nil {
				b.Error(err)

				return
			}
		}
	})
}

func BenchmarkParallelFindOne(b *testing.B) {
	db, raw := setup(b, ant.WithTracking(false))
	ctx := context.Background()

	// database/sql keeps 2 idle connections by default, so with more workers
	// than that most queries would close their connection and open a new one,
	// and the benchmark would measure the handshake. A server sizes its pool.
	for _, pool := range []*sql.DB{raw, db.DB()} {
		pool.SetMaxIdleConns(runtime.GOMAXPROCS(0))
	}

	b.Run("database-sql", func(b *testing.B) {
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				var u User

				err := raw.QueryRowContext(ctx, `SELECT id, created_at, updated_at, name, email, age, bio FROM users WHERE id = $1`, 500).
					Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.Name, &u.Email, &u.Age, &u.Bio)
				if err != nil {
					b.Error(err)

					return
				}
			}
		})
	})

	b.Run("ant", func(b *testing.B) {
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, err := db.Users.Find(ctx, int64(500)); err != nil {
					b.Error(err)

					return
				}
			}
		})
	})

	b.Run("ant-where", func(b *testing.B) {
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, err := db.Users.Where(ant.Eq(&Users.Age, 30), ant.Like(&Users.Name, "user%")).
					OrderBy(&Users.Name).
					First(ctx); err != nil {
					b.Error(err)

					return
				}
			}
		})
	})
}
