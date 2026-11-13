// Package secure stores values encrypted, with AES-256-GCM, so that a database
// dump reveals nothing. Set the keys once at startup:
//
//	secure.SetKeys("2026", map[string][]byte{"2026": newKey, "2025": oldKey})
//
// A model then uses the type:
//
//	type Patient struct {
//		ID  int64
//		SSN secure.Value[string]
//	}
//
// Rotation: add a new key, and make it active. Rows still read with the old key,
// and each write uses the new one. NeedsRotation says which values still need it.
//
// A value encrypts differently every time, so it cannot be compared or indexed in
// SQL. To look rows up, store a keyed hash beside it, as the guide describes.
package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/skudra-go/ant/internal/jsontext"
)

// Errors from reading and writing values.
var (
	ErrNoKeys     = errors.New("ant: secure.SetKeys has not been called")
	ErrUnknownKey = errors.New("ant: value was encrypted with an unknown key")
)

// keyring holds the keys SetKeys set, or nil before it is called. Every value
// read or written loads it, so it is swapped whole rather than locked, and a
// reader never sees one call's active key with another call's keys.
var keyring atomic.Pointer[ring]

type ring struct {
	active string
	aeads  map[string]cipher.AEAD
}

// SetKeys sets the keys, each 32 bytes, and which one new writes use. Names are
// stored with each value, so they must stay stable, and may not contain a colon.
func SetKeys(active string, keys map[string][]byte) error {
	aeads := make(map[string]cipher.AEAD, len(keys))

	for name, key := range keys {
		if strings.Contains(name, ":") {
			return fmt.Errorf("ant: key name %q has a colon", name)
		}

		if len(key) != 32 {
			return fmt.Errorf("ant: key %q has %d bytes, and needs 32", name, len(key))
		}

		block, err := aes.NewCipher(key)
		if err != nil {
			return err
		}

		if aeads[name], err = cipher.NewGCM(block); err != nil {
			return err
		}
	}

	if aeads[active] == nil {
		return fmt.Errorf("ant: active key %q is not among the keys", active)
	}

	keyring.Store(&ring{active, aeads})

	return nil
}

// Value holds a Go value that is encrypted in the database. A NULL column reads
// as the zero value.
type Value[V any] struct {
	V V
}

// New returns a Value holding v.
func New[V any](v V) Value[V] { return Value[V]{V: v} }

// ColumnType implements schema.Typed.
func (Value[V]) ColumnType(string) string { return "text" }

// String hides the value, so that a log line or a %v cannot leak it.
func (Value[V]) String() string { return "[encrypted]" }

// MarshalJSON hides the value, so that returning a model from an API cannot leak
// it. Read V to use it.
func (Value[V]) MarshalJSON() ([]byte, error) { return []byte(`"[encrypted]"`), nil }

// Value implements driver.Valuer. The output is key:base64(nonce+ciphertext).
// Text that is not valid UTF-8 is refused, since the JSON that is sealed would
// hold U+FFFD in its place.
func (s Value[V]) Value() (driver.Value, error) {
	keys := keyring.Load()
	if keys == nil {
		return nil, ErrNoKeys
	}

	// JSON would seal U+FFFD in place of text that is not valid UTF-8.
	if err := jsontext.Valid(s.V); err != nil {
		return nil, err
	}

	plain, err := json.Marshal(s.V)
	if err != nil {
		return nil, err
	}

	aead := keys.aeads[keys.active]

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	sealed := aead.Seal(nonce, nonce, plain, nil)

	return keys.active + ":" + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// Scan implements sql.Scanner.
func (s *Value[V]) Scan(src any) error {
	var zero V

	s.V = zero

	var text string

	switch src := src.(type) {
	case nil:
		return nil
	case string:
		text = src
	case []byte:
		text = string(src)
	default:
		return fmt.Errorf("ant: cannot scan %T into secure.Value", src)
	}

	plain, err := open(text)
	if err != nil {
		return err
	}

	return json.Unmarshal(plain, &s.V)
}

func open(text string) ([]byte, error) {
	name, body, ok := strings.Cut(text, ":")
	if !ok {
		return nil, errors.New("ant: value is not in secure form")
	}

	keys := keyring.Load()
	if keys == nil {
		return nil, ErrNoKeys
	}

	aead := keys.aeads[name]
	if aead == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKey, name)
	}

	sealed, err := base64.RawStdEncoding.DecodeString(body)
	if err != nil {
		return nil, err
	}

	if len(sealed) < aead.NonceSize() {
		return nil, errors.New("ant: value is too short")
	}

	return aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], nil)
}

// NeedsRotation reports whether a stored value used a key other than the active
// one. A SQL condition does the same across a table: column NOT LIKE 'active:%'.
func NeedsRotation(stored string) bool {
	name, _, _ := strings.Cut(stored, ":")

	active := ""

	if keys := keyring.Load(); keys != nil {
		active = keys.active
	}

	return name != active
}
