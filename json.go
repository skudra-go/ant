package ant

import (
	"bytes"
	"database/sql/driver"
	"encoding/gob"
	"encoding/json"
	"fmt"

	"github.com/skudra-go/ant/internal/jsontext"
)

// JSON stores a Go value in a JSON column. The zero value of V is stored as its
// JSON form, and a NULL column reads as the zero value. A number read into an
// interface, as in a map[string]any, is a json.Number, so an integer past 2^53
// keeps its digits when the value is written back.
type JSON[V any] struct {
	V V
}

// ColumnType implements schema.Typed.
func (j JSON[V]) ColumnType(dialect string) string {
	if dialect == "postgres" {
		return "jsonb"
	}

	return "json"
}

// MarshalJSON writes the value inside, so that an API response shows the document
// and not this wrapper.
func (j JSON[V]) MarshalJSON() ([]byte, error) { return json.Marshal(j.V) }

// UnmarshalJSON reads the value inside, as Scan does.
func (j *JSON[V]) UnmarshalJSON(data []byte) error { return jsontext.Unmarshal(data, &j.V) }

// Value implements driver.Valuer. Text that is not valid UTF-8 is refused, as a
// text column refuses it, since JSON would store U+FFFD in its place.
func (j JSON[V]) Value() (driver.Value, error) {
	if err := jsontext.Valid(j.V); err != nil {
		return nil, err
	}

	text, err := json.Marshal(j.V)
	if err != nil {
		return nil, err
	}

	return string(text), nil
}

// Scan implements sql.Scanner.
func (j *JSON[V]) Scan(src any) error {
	var zero V

	j.V = zero

	switch src := src.(type) {
	case nil:
		return nil
	case string:
		return jsontext.Unmarshal([]byte(src), &j.V)
	case []byte:
		return jsontext.Unmarshal(src, &j.V)
	}

	return fmt.Errorf("ant: cannot scan %T into JSON", src)
}

// Gob stores a Go value in a binary column with encoding/gob, for a value that only
// Go reads and JSON does not fit.
type Gob[V any] struct {
	V V
}

// ColumnType implements schema.Typed.
func (g Gob[V]) ColumnType(dialect string) string {
	if dialect == "postgres" {
		return "bytea"
	}

	return "blob"
}

// Value implements driver.Valuer.
func (g Gob[V]) Value() (driver.Value, error) {
	var b bytes.Buffer
	if err := gob.NewEncoder(&b).Encode(g.V); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// Scan implements sql.Scanner.
func (g *Gob[V]) Scan(src any) error {
	var zero V

	g.V = zero

	switch src := src.(type) {
	case nil:
		return nil
	case []byte:
		return gob.NewDecoder(bytes.NewReader(src)).Decode(&g.V)
	}

	return fmt.Errorf("ant: cannot scan %T into Gob", src)
}
