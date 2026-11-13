package secure

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestRoundTripAndRotation(t *testing.T) {
	oldKey, newKey := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
	if err := SetKeys("old", map[string][]byte{"old": oldKey}); err != nil {
		t.Fatal(err)
	}

	stored, err := New("secret").Value()
	if err != nil {
		t.Fatal(err)
	}

	text := stored.(string)
	if strings.Contains(text, "secret") {
		t.Fatal("plain text in the output")
	}

	again, _ := New("secret").Value()
	if again == stored {
		t.Fatal("the same value encrypted the same way twice")
	}

	if err := SetKeys("new", map[string][]byte{"old": oldKey, "new": newKey}); err != nil {
		t.Fatal(err)
	}

	var read Value[string]
	if err := read.Scan(text); err != nil || read.V != "secret" {
		t.Fatalf("after rotation: %q, %v", read.V, err)
	}

	if !NeedsRotation(text) {
		t.Fatal("old value not flagged")
	}

	fresh, _ := read.Value()
	if NeedsRotation(fresh.(string)) {
		t.Fatal("a new write used the old key")
	}

	if err := SetKeys("new", map[string][]byte{"new": newKey}); err != nil {
		t.Fatal(err)
	}

	if err := read.Scan(text); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("unknown key: %v", err)
	}
}

func TestTamperedValueFails(t *testing.T) {
	if err := SetKeys("k", map[string][]byte{"k": bytes.Repeat([]byte{3}, 32)}); err != nil {
		t.Fatal(err)
	}

	stored, _ := New(42).Value()
	text := stored.(string)
	tampered := text[:len(text)-2] + "AA"

	var read Value[int]
	if tampered != text && read.Scan(tampered) == nil {
		t.Fatal("tampering was not detected")
	}

	if err := read.Scan(nil); err != nil || read.V != 0 {
		t.Fatalf("NULL: %d, %v", read.V, err)
	}
}

func TestValueDoesNotLeak(t *testing.T) {
	v := New("secret")

	text, err := json.Marshal(struct{ SSN Value[string] }{v})
	if err != nil || strings.Contains(string(text), "secret") {
		t.Fatalf("JSON: %s, %v", text, err)
	}

	if strings.Contains(fmt.Sprint(v), "secret") {
		t.Fatal("printing showed the value")
	}
}

func TestSetKeysRejectsBadInput(t *testing.T) {
	good := bytes.Repeat([]byte{1}, 32)

	if SetKeys("a", map[string][]byte{"a": []byte("short")}) == nil {
		t.Fatal("short key accepted")
	}

	if SetKeys("a:b", map[string][]byte{"a:b": good}) == nil {
		t.Fatal("colon accepted")
	}

	if SetKeys("missing", map[string][]byte{"a": good}) == nil {
		t.Fatal("missing active key accepted")
	}
}

// Values written and read while SetKeys rotates the active key always use one
// call's keys: every value is sealed with a key it can be opened with. Run it
// with -race.
func TestRotationUnderConcurrency(t *testing.T) {
	keys := map[string][]byte{
		"a": bytes.Repeat([]byte{1}, 32),
		"b": bytes.Repeat([]byte{2}, 32),
	}

	if err := SetKeys("a", keys); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})

	var rotator sync.WaitGroup

	rotator.Go(func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}

			if err := SetKeys([]string{"a", "b"}[i%2], keys); err != nil {
				t.Error(err)

				return
			}
		}
	})

	var workers sync.WaitGroup

	for w := range 8 {
		workers.Go(func() {
			for i := range 200 {
				want := fmt.Sprint(w, "-", i)

				stored, err := New(want).Value()
				if err != nil {
					t.Error(err)

					return
				}

				var read Value[string]
				if err := read.Scan(stored); err != nil || read.V != want {
					t.Errorf("read %q, %v, want %q", read.V, err, want)

					return
				}

				_ = NeedsRotation(stored.(string))
			}
		})
	}

	workers.Wait()
	close(stop)
	rotator.Wait()
}
