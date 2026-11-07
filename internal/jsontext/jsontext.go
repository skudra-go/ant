// Package jsontext checks a value before encoding/json writes it.
package jsontext

import (
	"encoding"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"
)

// ErrInvalidUTF8 is returned for text that encoding/json would change: it writes
// each byte of invalid UTF-8 as U+FFFD, without an error, so what is stored
// would not be what was written. A text column refuses such text too.
var ErrInvalidUTF8 = errors.New("ant: text that is not valid UTF-8 cannot be stored as JSON")

// maxDepth bounds the walk, as encoding/json bounds its own, so a value that
// holds itself is left to it to report.
const maxDepth = 1000

var (
	marshaler     = reflect.TypeFor[json.Marshaler]()
	textMarshaler = reflect.TypeFor[encoding.TextMarshaler]()
)

// Valid returns ErrInvalidUTF8 if v holds a string, or a map key, that is not
// valid UTF-8 where encoding/json would write it as text. A value that marshals
// itself writes what it writes, and a []byte goes as base64, so neither is
// looked into.
func Valid(v any) error {
	if !valid(reflect.ValueOf(v), 0) {
		return ErrInvalidUTF8
	}

	return nil
}

func valid(v reflect.Value, depth int) bool {
	if !v.IsValid() || depth > maxDepth {
		return true
	}

	if t := v.Type(); t.Implements(marshaler) || t.Implements(textMarshaler) {
		return true
	}

	switch v.Kind() {
	case reflect.String:
		return utf8.ValidString(v.String())
	case reflect.Pointer, reflect.Interface:
		return v.IsNil() || valid(v.Elem(), depth+1)
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return true
		}

		fallthrough
	case reflect.Array:
		for i := range v.Len() {
			if !valid(v.Index(i), depth+1) {
				return false
			}
		}
	case reflect.Map:
		for iter := v.MapRange(); iter.Next(); {
			if key := iter.Key(); key.Kind() == reflect.String && !utf8.ValidString(key.String()) {
				return false
			}

			if !valid(iter.Value(), depth+1) {
				return false
			}
		}
	case reflect.Struct:
		for i := range v.NumField() {
			field := v.Type().Field(i)
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")

			if !field.IsExported() && !field.Anonymous || name == "-" {
				continue
			}

			if !valid(v.Field(i), depth+1) {
				return false
			}
		}
	}

	return true
}
