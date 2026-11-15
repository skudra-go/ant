package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// An array column can hold NULL elements, which a slice of plain values cannot:
// reading one as "NULL", false, or zero would write that back on the next save.
func TestArrayScanRefusesNullElements(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		var a Array[string]

		for _, text := range []string{`{x,NULL,"NULL"}`, `{NULL}`, `{null}`, `{x,Null}`, `{NULL,x}`} {
			require.ErrorContains(t, a.Scan(text), "NULL element", text)
		}

		require.Nil(t, a, "a failed scan leaves the value alone")
	})

	t.Run("booleans and numbers", func(t *testing.T) {
		var (
			b Array[bool]
			i Array[int32]
			n Array[int]
			l Array[int64]
			f Array[float64]
		)

		require.ErrorContains(t, b.Scan("{t,NULL}"), "NULL element")
		require.ErrorContains(t, i.Scan("{1,NULL}"), "NULL element")
		require.ErrorContains(t, n.Scan([]byte("{NULL,1}")), "NULL element")
		require.ErrorContains(t, l.Scan("{NULL}"), "NULL element")
		require.ErrorContains(t, f.Scan("{1.5,NULL}"), "NULL element")
	})

	t.Run("a quoted NULL is text", func(t *testing.T) {
		var a Array[string]

		require.NoError(t, a.Scan(`{"NULL","null",NULLS,"",x}`))
		require.Equal(t, Array[string]{"NULL", "null", "NULLS", "", "x"}, a)
	})

	t.Run("a NULL array is nil, and an empty one is empty", func(t *testing.T) {
		a := Array[string]{"x"}

		require.NoError(t, a.Scan(nil))
		require.Nil(t, a)
		require.NoError(t, a.Scan("{}"))
		require.NotNil(t, a)
		require.Empty(t, a)
	})
}
