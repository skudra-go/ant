package ant_test

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
)

// A number inside an interface reads as a json.Number, so an integer past 2^53
// keeps its digits however the JSON reaches the value: scanned as text or bytes,
// or decoded from a request body (#201).
func TestJSONKeepsLargeIntegers(t *testing.T) {
	const doc = `{"note": "a", "order_id": 9007199254740993, "ids": [18446744073709551615]}`

	decoders := map[string]func(*ant.JSON[map[string]any]) error{
		"Scan of a string": func(j *ant.JSON[map[string]any]) error {
			return j.Scan(doc)
		},
		"Scan of bytes": func(j *ant.JSON[map[string]any]) error {
			return j.Scan([]byte(doc))
		},
		"UnmarshalJSON": func(j *ant.JSON[map[string]any]) error {
			return json.Unmarshal([]byte(doc), j)
		},
	}

	for name, decode := range decoders {
		t.Run(name, func(t *testing.T) {
			var j ant.JSON[map[string]any]

			require.NoError(t, decode(&j))
			require.Equal(t, json.Number("9007199254740993"), j.V["order_id"])
			require.Equal(t, []any{json.Number("18446744073709551615")}, j.V["ids"])

			j.V["note"] = "b"

			written, err := j.Value()
			require.NoError(t, err)

			require.JSONEq(
				t,
				`{"note": "b", "order_id": 9007199254740993, "ids": [18446744073709551615]}`,
				written.(string),
			)
		})
	}

	t.Run("a typed field is unchanged", func(t *testing.T) {
		var j ant.JSON[struct {
			ID    int64
			Ratio float64
		}]

		require.NoError(t, j.Scan(`{"ID": 9007199254740993, "Ratio": 0.5}`))
		require.EqualValues(t, 9007199254740993, j.V.ID)
		require.InDelta(t, 0.5, j.V.Ratio, 0)
	})
}

// Scan refuses what json.Unmarshal refuses, so decoding with a stream changed no
// error into a success.
func TestJSONScanRefusesWhatUnmarshalRefuses(t *testing.T) {
	for name, text := range map[string]string{
		"empty":                   "",
		"only space":              "  ",
		"cut short":               `{"a": 1`,
		"text after the value":    `{"a": 1} x`,
		"a second value":          `{"a": 1} {"b": 2}`,
		"a closing bracket after": `{"a": 1}]`,
		"a wrong type":            `["a"]`,
	} {
		t.Run(name, func(t *testing.T) {
			var j ant.JSON[map[string]any]

			require.Error(t, json.Unmarshal([]byte(text), &map[string]any{}))
			require.Error(t, j.Scan(text))
			require.Error(t, j.Scan([]byte(text)))
		})
	}

	t.Run("space around the value", func(t *testing.T) {
		var j ant.JSON[map[string]any]

		require.NoError(t, j.Scan(" \n{\"a\": 1}\t "))
		require.Equal(t, json.Number("1"), j.V["a"])
	})
}

// ant.DeletedAt reads and writes JSON as a *time.Time does: null for a live row,
// and the time for a deleted one. It still reads the {"V","Valid"} object it was
// written as before (#299).
func TestDeletedAtJSON(t *testing.T) {
	type row struct {
		ID        int64
		DeletedAt ant.DeletedAt
	}

	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	gone := ant.DeletedAt{Null: sql.Null[time.Time]{V: when, Valid: true}}

	live, err := json.Marshal(row{ID: 1})
	require.NoError(t, err)
	require.JSONEq(t, `{"ID":1,"DeletedAt":null}`, string(live))

	deleted, err := json.Marshal(row{ID: 2, DeletedAt: gone})
	require.NoError(t, err)
	require.JSONEq(t, `{"ID":2,"DeletedAt":"2026-01-02T03:04:05Z"}`, string(deleted))

	for doc, want := range map[string]ant.DeletedAt{
		`{"DeletedAt":null}`:                   {},
		`{}`:                                   {},
		`{"DeletedAt":"2026-01-02T03:04:05Z"}`: gone,
		`{"DeletedAt":{"V":"2026-01-02T03:04:05Z","Valid":true}}`:  gone,
		`{"DeletedAt":{"V":"0001-01-01T00:00:00Z","Valid":false}}`: {},
	} {
		var got row
		require.NoError(t, json.Unmarshal([]byte(doc), &got), doc)
		require.True(t, got.DeletedAt.V.Equal(want.V), doc)
		require.Equal(t, want.Valid, got.DeletedAt.Valid, doc)
	}

	t.Run("null makes a deleted row live", func(t *testing.T) {
		got := row{DeletedAt: gone}
		require.NoError(t, json.Unmarshal([]byte(`{"DeletedAt":null}`), &got))
		require.False(t, got.DeletedAt.Valid)
	})

	t.Run("a value that is not a time is an error", func(t *testing.T) {
		var got row
		require.Error(t, json.Unmarshal([]byte(`{"DeletedAt":"yesterday"}`), &got))
		require.Error(t, json.Unmarshal([]byte(`{"DeletedAt":5}`), &got))
	})
}
