package meta

import (
	"math"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

type wideKeyed struct {
	ID   uint64
	Name string
}

type uintKeyed struct {
	ID uint
}

// A key that fits an int64 is one, whatever integer holds it, so an int64 and a
// uint64 that hold the same number are the same key. A uint past math.MaxInt64
// is its uint64: as an int64 it wrapped to a negative number, which KeyOf gave
// to foreign keys and to conflicts' errors (#206).
func TestUnsignedKeys(t *testing.T) {
	big := uint64(math.MaxInt64) + 7

	for _, c := range []struct {
		value any
		want  any
	}{
		{uint64(7), int64(7)},
		{uint(7), int64(7)},
		{uint32(math.MaxUint32), int64(math.MaxUint32)},
		{uint64(math.MaxInt64), int64(math.MaxInt64)},
		{big, big},
		{uint64(math.MaxUint64), uint64(math.MaxUint64)},
		{uint(math.MaxUint64), uint64(math.MaxUint64)},
		{&big, big},
	} {
		got, ok := KeyValue(c.value)
		require.True(t, ok)
		require.Equal(t, c.want, got, "%T %v", c.value, c.value)
	}

	model := Of[wideKeyed]()

	for _, c := range []struct {
		id   uint64
		want any
		fast bool
	}{
		{7, int64(7), true},
		{big, big, false},
	} {
		entity := &wideKeyed{ID: c.id}

		key, ok := model.KeyOf(unsafe.Pointer(entity))
		require.True(t, ok)
		require.Equal(t, c.want, key)

		n, ok, fast := model.IntKey(unsafe.Pointer(entity))
		require.Equal(t, c.fast, fast, "IntKey answers only for a key that fits")

		if fast {
			require.True(t, ok)
			require.Equal(t, c.want, n)
		}
	}

	key, ok := Of[uintKeyed]().KeyOf(unsafe.Pointer(&uintKeyed{ID: math.MaxUint64}))
	require.True(t, ok)
	require.Equal(t, uint64(math.MaxUint64), key)
}
