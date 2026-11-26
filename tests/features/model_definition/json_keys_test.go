package model_definition

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect/postgres"
	"github.com/skudra-go/ant/metrics"
)

// Note embeds ant.Base, as most models do.
type Note struct {
	ant.Base
	Text string `json:"text"`
}

// The structs ant hands back encode with camelCase keys, as JSON APIs expect, so
// a model that embeds ant.Base answers {"id":1,"createdAt":...} and a page can be
// returned as it is (#308).
func TestJSONKeysAreCamelCase(t *testing.T) {
	at := time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC)
	note := &Note{Base: ant.Base{ID: 1, CreatedAt: at, UpdatedAt: at}, Text: "hi"}
	stamp := `"2026-11-26T08:00:00Z"`
	noteJSON := `{"id":1,"createdAt":` + stamp + `,"updatedAt":` + stamp + `,"text":"hi"}`

	for _, c := range []struct {
		name  string
		value any
		want  string
	}{
		{"a model that embeds Base", note, noteJSON},
		{
			"a page",
			ant.Page[Note]{
				Items:  []*Note{note},
				Total:  3,
				Number: 2,
				Size:   1,
			},
			`{"items":[` + noteJSON + `],"total":3,"number":2,"size":1}`,
		},
		{
			"a page of a cursor",
			ant.CursorPage[Note]{Items: []*Note{}, Next: "abc"},
			`{"items":[],"next":"abc"}`,
		},
		{"a point", postgres.Point{X: 1, Y: 2}, `{"x":1,"y":2}`},
		{
			"a partition",
			postgres.Partition{Name: "events_2026_11", Month: at},
			`{"name":"events_2026_11","month":` + stamp + `}`,
		},
		{
			"a metrics sample",
			metrics.Sample{
				Kind:     "select",
				Count:    2,
				Errors:   1,
				Duration: time.Second,
			},
			`{"kind":"select","count":2,"errors":1,"duration":1000000000}`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			text, err := json.Marshal(c.value)
			require.NoError(t, err)
			require.JSONEq(t, c.want, string(text))

			// A request body in the same keys reads back into the same value.
			back := reflect.New(reflect.TypeOf(c.value))
			require.NoError(t, json.Unmarshal([]byte(c.want), back.Interface()))
			require.Equal(t, c.value, back.Elem().Interface())
		})
	}

	t.Run("keys written before still read", func(t *testing.T) {
		var read Note
		require.NoError(t, json.Unmarshal([]byte(`{"ID":7,"CreatedAt":`+stamp+`}`), &read))
		require.Equal(t, int64(7), read.ID)
		require.Equal(t, at, read.CreatedAt)
	})
}
