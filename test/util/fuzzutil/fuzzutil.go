// Package fuzzutil turns a fuzz input into structured values. Getters read
// big-endian integers and return zero values once the input is exhausted, so a
// harness never has to check lengths. Record gives each operation a fixed
// width, so editing one operation does not shift the bytes of later ones.
package fuzzutil

import (
	"encoding/binary"
	"fmt"
)

// Consumer reads values from the front of a fuzz input.
type Consumer struct{ b []byte }

// New returns a Consumer over data. It does not copy data.
func New(data []byte) *Consumer { return &Consumer{b: data} }

// Remaining returns the number of unread bytes.
func (c *Consumer) Remaining() int { return len(c.b) }

// Exhausted reports whether every byte has been read.
func (c *Consumer) Exhausted() bool { return len(c.b) == 0 }

// take returns the next n bytes, zero-padded when fewer remain.
func (c *Consumer) take(n int) []byte {
	if n <= len(c.b) {
		v := c.b[:n:n]
		c.b = c.b[n:]
		return v
	}
	v := make([]byte, n)
	copy(v, c.b)
	c.b = nil
	return v
}

// Byte returns the next byte.
func (c *Consumer) Byte() byte { return c.take(1)[0] }

// Bool returns whether the low bit of the next byte is set.
func (c *Consumer) Bool() bool { return c.Byte()&1 == 1 }

// Uint16 returns the next two bytes as a big-endian integer.
func (c *Consumer) Uint16() uint16 { return binary.BigEndian.Uint16(c.take(2)) }

// Uint32 returns the next four bytes as a big-endian integer.
func (c *Consumer) Uint32() uint32 { return binary.BigEndian.Uint32(c.take(4)) }

// Uint64 returns the next eight bytes as a big-endian integer.
func (c *Consumer) Uint64() uint64 { return binary.BigEndian.Uint64(c.take(8)) }

// Intn returns a value in [0, n), reading one byte when n <= 256 and four
// otherwise. It returns 0 without reading when n <= 1.
func (c *Consumer) Intn(n int) int {
	switch {
	case n <= 1:
		return 0
	case n <= 1<<8:
		return int(c.Byte()) % n
	default:
		return int(uint64(c.Uint32()) % uint64(n))
	}
}

// Bytes reads a one-byte length prefix and returns that many bytes, capped at
// max and at the bytes that remain. The result aliases the input.
func (c *Consumer) Bytes(maxLen int) []byte {
	if maxLen <= 0 {
		return nil
	}
	n := min(int(c.Byte()), maxLen, len(c.b))
	v := c.b[:n:n]
	c.b = c.b[n:]
	return v
}

// Record returns a Consumer over the next width bytes, zero-padded when fewer
// remain, and advances past them. Reading more than width bytes from the
// record yields zero values rather than bytes of the next record.
func (c *Consumer) Record(width int) *Consumer {
	if width <= 0 {
		return &Consumer{}
	}
	return &Consumer{b: c.take(width)}
}

// Records concatenates records, zero-padding each to width bytes. Seed inputs
// use it to lay out one operation per record. It panics on a record longer
// than width, which is a bug in the seed.
func Records(width int, records ...[]byte) []byte {
	out := make([]byte, 0, width*len(records))
	for i, r := range records {
		if len(r) > width {
			panic(fmt.Sprintf("fuzzutil: record %d has %d bytes, want at most %d", i, len(r), width))
		}
		out = append(out, r...)
		out = append(out, make([]byte, width-len(r))...)
	}
	return out
}
