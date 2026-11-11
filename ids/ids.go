// Package ids makes unique keys in Go, for use with ant.GenerateKey:
//
//	func (Order) Configure(b *ant.ModelBuilder[Order]) {
//		ant.GenerateKey(b, &Orders.ID, ids.UUID)
//	}
//
// UUID and ULID sort by creation time, unlike a random UUID, so new rows land at
// the end of an index instead of scattering across it.
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// UUID returns a version 7 UUID in its usual text form: a millisecond timestamp
// followed by random bits, so values sort by time.
func UUID() string {
	var b [16]byte
	fillTime(b[:6])
	rand.Read(b[6:])
	b[6] = b[6]&0x0f | 0x70
	b[8] = b[8]&0x3f | 0x80

	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])

	return string(out[:])
}

func fillTime(b []byte) {
	ms := time.Now().UnixMilli()

	for i := len(b) - 1; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
}

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ULID returns a 26-character ULID: a millisecond timestamp and random bits in
// Crockford base 32, which sorts by time as text.
func ULID() string {
	var b [16]byte
	fillTime(b[:6])
	rand.Read(b[6:])

	var out [26]byte
	// 128 bits in 26 characters of 5 bits each, with 2 spare bits at the top.
	var acc uint32

	bits := 2
	i := 0

	for _, c := range b {
		acc = acc<<8 | uint32(c)

		bits += 8
		for bits >= 5 {
			bits -= 5
			out[i] = crockford[(acc>>bits)&31]
			i++
		}
	}

	return string(out[:])
}

// Snowflake makes 63-bit integer keys that sort by time: 41 bits of milliseconds
// since an epoch, 10 bits of node, and 12 bits of sequence. Run one per node. It is
// safe for concurrent use, and makes up to 4096 keys per millisecond per node.
type Snowflake struct {
	mu       sync.Mutex
	node     int64
	epoch    int64
	last     int64
	sequence int64
}

// NewSnowflake returns a generator for a node from 0 to 1023. Times count from
// 2024, which leaves about 69 years.
func NewSnowflake(node int64) (*Snowflake, error) {
	if node < 0 || node > 1023 {
		return nil, errors.New("ant: snowflake node must be from 0 to 1023")
	}

	return &Snowflake{
		node:  node,
		epoch: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
	}, nil
}

// Next returns the next key. If the clock went backwards, it waits to catch up.
func (s *Snowflake) Next() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()
	for now < s.last {
		time.Sleep(time.Duration(s.last-now) * time.Millisecond)
		now = time.Now().UnixMilli()
	}

	if now == s.last {
		s.sequence = (s.sequence + 1) & 4095

		if s.sequence == 0 {
			for now <= s.last {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		s.sequence = 0
	}

	s.last = now

	return (now-s.epoch)<<22 | s.node<<12 | s.sequence
}
