package ant

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite" // driver "sqlite"

	"github.com/skudra-go/ant/dialect/sqlite"
)

type externalUser struct {
	ID   int64
	Name string
}

type externalDB struct {
	Context
	Users Table[externalUser]
}

// A transaction from UseTx ends where ant cannot see it, so nothing waits for
// its end: the changes ant saves in it are kept at once, and observers, which
// never run for it, are not queued. Only a savepoint ant opens in it holds its
// changes, until it is released or rolled back.
func TestUseTxHoldsNothingItCannotSettle(t *testing.T) {
	ctx := t.Context()
	dsn := "file:" + filepath.Join(t.TempDir(), "ant.db")

	db, err := Open[externalDB](ctx, sqlite.Dialect, dsn, WithTracking(true))
	if err != nil {
		t.Fatal(err)
	}

	defer db.Close()

	if _, err := db.DB().ExecContext(
		ctx,
		"CREATE TABLE external_users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)",
	); err != nil {
		t.Fatal(err)
	}

	observed := 0

	if err := Observe(db, Observer[externalUser]{
		Inserted: func(context.Context, *externalUser) { observed++ },
	}); err != nil {
		t.Fatal(err)
	}

	raw, err := db.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	defer raw.Rollback()

	tx, err := UseTx(db, raw)
	if err != nil {
		t.Fatal(err)
	}

	state := tx.state.tx
	held := func() int { return len(state.undo) + len(state.pending) }

	for i := range 3 {
		u := &externalUser{Name: fmt.Sprint("outside ", i)}
		if err := Add(ctx, tx, u); err != nil {
			t.Fatal(err)
		}

		if err := tx.SaveChanges(ctx); err != nil {
			t.Fatal(err)
		}
	}

	if held() != 0 {
		t.Fatalf("held %d changes and observers for a transaction ant cannot see end", held())
	}

	inner := &externalUser{Name: "inner"}

	err = tx.Tx(ctx, func(tx *externalDB) error {
		if err := Add(ctx, tx, inner); err != nil {
			return err
		}

		if err := tx.SaveChanges(ctx); err != nil {
			return err
		}

		if held() == 0 {
			t.Error("a savepoint of ant's own held nothing to undo")
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if held() != 0 {
		t.Errorf("held %d once the savepoint was released", held())
	}

	failed := &externalUser{Name: "failed"}
	stop := errors.New("roll back to the savepoint")

	err = tx.Tx(ctx, func(tx *externalDB) error {
		if err := Add(ctx, tx, failed); err != nil {
			return err
		}

		if err := tx.SaveChanges(ctx); err != nil {
			return err
		}

		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatal(err)
	}

	entry, err := Entry(ctx, db, failed)
	if err != nil {
		t.Fatal(err)
	}

	if got := entry.State(); got != Added || failed.ID != 0 {
		t.Errorf("the savepoint's rollback left the insert %s, with key %d", got, failed.ID)
	}

	if held() != 0 || observed != 0 {
		t.Errorf("held %d, and %d observers ran", held(), observed)
	}
}
