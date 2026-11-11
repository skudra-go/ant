package relations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// A relation is a field: []Post is one-to-many, and *User is many-to-one or
// one-to-one. The foreign key is found by name, UserID here, on either side.
type Person struct {
	ID       int64
	Name     string
	Passport *Passport // one-to-one: Passport.PersonID
	Pets     []Pet     // one-to-many: Pet.PersonID
}

type Passport struct {
	ID       int64
	PersonID int64
	Number   string
}

type Pet struct {
	ID       int64
	PersonID int64
	Name     string
	Person   *Person // many-to-one: Pet.PersonID
}

type HomeDB struct {
	ant.Context
	People    ant.Table[Person]
	Passports ant.Table[Passport]
	Pets      ant.Table[Pet]
}

const homeDDL = `
	CREATE TABLE persons (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL);
	CREATE TABLE passports (id BIGSERIAL PRIMARY KEY, person_id BIGINT NOT NULL REFERENCES persons, number TEXT NOT NULL);
	CREATE TABLE pets (id BIGSERIAL PRIMARY KEY, person_id BIGINT NOT NULL REFERENCES persons, name TEXT NOT NULL)`

var (
	Persons = ant.Fields[Person]()
	Pets    = ant.Fields[Pet]()
)

func TestConventions(t *testing.T) {
	fixture.RunWith(t, homeDDL, func(t *testing.T, db *HomeDB) {
		ctx := t.Context()
		ann := &Person{Name: "Ann"}
		require.NoError(t, db.People.Insert(ctx, ann))
		require.NoError(t, db.Passports.Insert(ctx, &Passport{PersonID: ann.ID, Number: "LV1"}))
		require.NoError(t, db.Pets.Insert(ctx, &Pet{PersonID: ann.ID, Name: "Rex"}))

		t.Run("one to many", func(t *testing.T) {
			found, err := db.People.Include(&Persons.Pets).First(ctx)
			require.NoError(t, err)
			require.Equal(t, "Rex", found.Pets[0].Name)
		})

		t.Run("many to one", func(t *testing.T) {
			found, err := db.Pets.IncludeOne(&Pets.Person).First(ctx)
			require.NoError(t, err)
			require.Equal(t, "Ann", found.Person.Name)
		})

		t.Run("one to one", func(t *testing.T) {
			found, err := db.People.IncludeOne(&Persons.Passport).First(ctx)
			require.NoError(t, err)
			require.Equal(t, "LV1", found.Passport.Number)
		})

		t.Run("relation fields are not columns", func(t *testing.T) {
			found, err := db.People.First(ctx)
			require.NoError(t, err)
			require.Nil(t, found.Pets)
			require.Nil(t, found.Passport)
		})
	})
}
