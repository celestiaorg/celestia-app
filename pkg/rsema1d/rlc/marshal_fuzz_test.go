package rlc_test

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/rlc"
)

// FuzzUnmarshal checks rlc.Unmarshal on RLC bytes received from fibre upload
// peers and download servers. It must accept exactly the lengths that are a
// multiple of GF128Size, never panic, and round-trip accepted bytes through
// Marshal. Decode must reject any destination of the wrong size.
func FuzzUnmarshal(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, field.GF128Size))
	f.Add(bytes.Repeat([]byte{0xff}, 4*field.GF128Size))
	f.Add(rlc.Marshal(rlc.Vector{{1, 2, 3, 4, 5, 6, 7, 8}, {0xffff, 0, 0x8000, 1, 2, 3, 4, 5}}))
	f.Add(make([]byte, field.GF128Size+1))
	f.Add([]byte{1})

	f.Fuzz(func(t *testing.T, src []byte) {
		values, err := rlc.Unmarshal(src)
		if (err == nil) != (len(src)%field.GF128Size == 0) {
			t.Fatalf("Unmarshal of %d bytes: err=%v", len(src), err)
		}
		if err != nil {
			return
		}
		if len(values) != len(src)/field.GF128Size {
			t.Fatalf("Unmarshal returned %d values for %d bytes", len(values), len(src))
		}
		if !bytes.Equal(rlc.Marshal(values), src) {
			t.Fatal("Marshal(Unmarshal(src)) != src")
		}
		if err := rlc.Decode(make(rlc.Vector, len(values)+1), src); err == nil {
			t.Fatal("Decode accepted a destination one element too long")
		}
		if len(values) > 0 {
			if err := rlc.Decode(make(rlc.Vector, len(values)-1), src); err == nil {
				t.Fatal("Decode accepted a destination one element too short")
			}
		}
	})
}
