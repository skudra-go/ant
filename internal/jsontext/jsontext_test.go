package jsontext

import (
	"encoding/json"
	"errors"
	"testing"
)

type upper string

func (u upper) MarshalText() ([]byte, error) { return []byte("x"), nil }

type doc struct {
	Name   string
	Hidden string `json:"-"`
	secret string
	Raw    []byte
	Inner  *doc
	Any    any
	Text   upper
}

func TestValid(t *testing.T) {
	bad := "a\xffb"

	for name, tt := range map[string]struct {
		v     any
		valid bool
	}{
		"nil":                  {nil, true},
		"text":                 {"日本 ok", true},
		"invalid text":         {bad, false},
		"a map key":            {map[string]int{bad: 1}, false},
		"a map value":          {map[string]any{"k": []any{"ok", bad}}, false},
		"a struct field":       {doc{Name: bad}, false},
		"a field json leaves":  {doc{Hidden: bad}, true},
		"an unexported field":  {doc{secret: bad}, true},
		"bytes, as base64":     {doc{Raw: []byte(bad)}, true},
		"behind a pointer":     {doc{Inner: &doc{Name: bad}}, false},
		"inside an any":        {doc{Any: map[string]any{"x": bad}}, false},
		"a type that marshals": {doc{Text: upper(bad)}, true},
		"a raw message":        {json.RawMessage(`"ok"`), true},
		"an array of text":     {[2]string{"ok", bad}, false},
	} {
		t.Run(name, func(t *testing.T) {
			err := Valid(tt.v)
			if tt.valid != (err == nil) || err != nil && !errors.Is(err, ErrInvalidUTF8) {
				t.Errorf("Valid = %v, want valid %v", err, tt.valid)
			}
		})
	}

	self := map[string]any{}
	self["me"] = self

	if err := Valid(self); err != nil {
		t.Errorf("a value that holds itself is left to encoding/json: %v", err)
	}
}
