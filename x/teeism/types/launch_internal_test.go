package types

import (
	"bytes"
	"testing"

	testifyrequire "github.com/stretchr/testify/require"
)

// TestCheckTdLaunch pins which launches a quote may come from. Our enclave's
// attributes are 0x0000001000000000 (SEPT_VE_DISABLE only), from the real quote
// TestVerifyRealQuote verifies end to end.
func TestCheckTdLaunch(t *testing.T) {
	ours := []byte{0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 0x00}
	with := func(byteIdx int, mask byte) []byte {
		a := bytes.Clone(ours)
		a[byteIdx] |= mask
		return a
	}
	zero48 := make([]byte, 48)
	bound := bytes.Clone(zero48)
	bound[47] = 1

	testifyrequire.NoError(t, checkTdLaunch(ours, nil), "a v4 or TD 1.0 body carries no mr_servicetd")
	testifyrequire.NoError(t, checkTdLaunch(ours, zero48), "a TD 1.5 body with no service td bound")

	testifyrequire.ErrorContains(t, checkTdLaunch(with(0, 0x01), nil), "debug")
	testifyrequire.ErrorContains(t, checkTdLaunch(with(3, 0x20), nil), "migratable")
	testifyrequire.ErrorContains(t, checkTdLaunch(ours, bound), "service td")
	testifyrequire.ErrorContains(t, checkTdLaunch(ours[:7], nil), "8 bytes")

	// Bits we take no view on, such as PKS (bit 30), still pass.
	testifyrequire.NoError(t, checkTdLaunch(with(3, 0x40), zero48))
}
