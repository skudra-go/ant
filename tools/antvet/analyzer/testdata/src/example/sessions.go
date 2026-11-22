package example

import (
	"context"

	"github.com/skudra-go/ant"
)

type AppDB struct {
	ant.Context
	Users ant.Table[User]
}

// An entity loaded through the context a session came from, and changed, is not
// saved by the session's SaveChanges, which saves only what the session tracks
// (#153).
func throughRoot(ctx context.Context, db *AppDB) error {
	s := ant.Session(db)

	u, _ := db.Users.Find(ctx, 1)
	u.Age = 50

	return s.SaveChanges(ctx) // want `u was loaded through db and changed, but s.SaveChanges saves only what s tracks`
}

// Through a query, as through Find.
func throughQuery(ctx context.Context, db *AppDB) error {
	s := ant.Session(db)

	u, err := db.Users.Where().First(ctx)
	if err != nil {
		return err
	}

	u.Name = "x"

	return s.SaveChanges(ctx) // want `u was loaded through db`
}

// Nor by another session's.
func twoSessions(ctx context.Context, db *AppDB) error {
	a, b := ant.Session(db), ant.Session(db)

	u, _ := a.Users.First(ctx)
	u.Age++

	return b.SaveChanges(ctx) // want `u was loaded through a and changed, but b.SaveChanges`
}

// Nor by the context the session came from.
func throughSession(ctx context.Context, db *AppDB) error {
	s := ant.Session(db)

	u, _ := s.Users.Find(ctx, 1)
	u.Age = 1

	return db.SaveChanges(ctx) // want `u was loaded through s`
}

// The same session saves it.
func sameSession(ctx context.Context, db *AppDB) error {
	s := ant.Session(db)

	u, _ := s.Users.Find(ctx, 1)
	u.Age = 1

	return s.SaveChanges(ctx)
}

// An entity attached to the session, or passed anywhere that might attach or
// save it, is left alone.
func attached(ctx context.Context, db *AppDB) error {
	s := ant.Session(db)

	u, _ := db.Users.Find(ctx, 1)
	u.Age = 1

	if err := ant.Attach(ctx, s, u); err != nil {
		return err
	}

	return s.SaveChanges(ctx)
}

func passedOn(ctx context.Context, db *AppDB, keep func(*User)) error {
	s := ant.Session(db)

	u, _ := db.Users.Find(ctx, 1)
	u.Age = 1
	keep(u)

	return s.SaveChanges(ctx)
}

// Reading an entity, or changing it after the save, saves nothing either way.
func onlyRead(ctx context.Context, db *AppDB) (int, error) {
	s := ant.Session(db)

	u, _ := db.Users.Find(ctx, 1)

	return u.Age, s.SaveChanges(ctx)
}

func changedAfter(ctx context.Context, db *AppDB) error {
	s := ant.Session(db)

	u, _ := db.Users.Find(ctx, 1)
	err := s.SaveChanges(ctx)
	u.Age = 1

	return err
}

// Handles whose trackers the analyzer cannot tell apart are left alone: a
// transaction from Begin shares its context's tracker, and two parameters may
// be one context.
func inTransaction(ctx context.Context, db *AppDB) error {
	s := ant.Session(db)

	tx, _ := ant.Begin(ctx, s)
	u, _ := tx.Users.Find(ctx, 1)
	u.Age = 1

	return s.SaveChanges(ctx)
}

func twoParams(ctx context.Context, a, b *AppDB) error {
	u, _ := a.Users.Find(ctx, 1)
	u.Age = 1

	return b.SaveChanges(ctx)
}

// A handle given another value may be any context by the time it saves.
func reassignedHandle(ctx context.Context, db *AppDB) error {
	s := ant.Session(db)
	s = db

	u, _ := db.Users.Find(ctx, 1)
	u.Age = 1

	return s.SaveChanges(ctx)
}

// A closure runs when its caller says, so loads in one and saves in another are
// not compared.
func closures(ctx context.Context, db *AppDB) error {
	s := ant.Session(db)

	var u *User

	load := func() { u, _ = db.Users.Find(ctx, 1) }
	load()
	u.Age = 1

	return s.SaveChanges(ctx)
}

// A root that tracks saves what it loaded itself, so its own SaveChanges after
// the change is the one that counts.
func rootSavesToo(ctx context.Context, db *AppDB) error {
	s := ant.Session(db)

	u, _ := db.Users.Find(ctx, 1)
	u.Age = 1

	if err := s.SaveChanges(ctx); err != nil {
		return err
	}

	return db.SaveChanges(ctx)
}

// Each save is reported once, naming the first entity it leaves out.
func twoEntities(ctx context.Context, db *AppDB) error {
	s := ant.Session(db)

	u, _ := db.Users.Find(ctx, 1)
	v, _ := db.Users.Find(ctx, 2)
	u.Age = 1
	v.Age = 2

	return s.SaveChanges(ctx) // want `^u was loaded through db`
}
