package rsema1d

import (
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
)

// This file implements a per-row log/exp LUT fast path for computeRLC.
//
// Motivation
// ----------
// klauspost's reedsolomon.(LowLevel).GF16Mul is implemented as:
//
//	func (l LowLevel) GF16Mul(a, b uint16) uint16 {
//	    initConstants()
//	    if a == 0 || b == 0 { return 0 }
//	    logSum := addMod(logLUT[ffe(a)], logLUT[ffe(b)])
//	    if logSum >= modulus { logSum -= modulus }
//	    return uint16(expLUT[logSum])
//	}
//
// initConstants() takes a sync.Once load+branch on every call.  For the hot
// RLC loop that's 8 Mul per symbol * 16384 symbols per row = ~131k calls, and
// the once-load shows up at ~9% on fibre flame graphs.  Additionally the
// per-call logLUT[a] lookup is redundant: all 8 component multiplies in the
// inner loop share the same symbol `sym`, so logSym = logLUT[sym] can be
// hoisted — 9 log lookups per symbol become 1.
//
// And because the coeffs for a given row are fixed, we can precompute
// coeffLog[j*8+k] = logLUT[coeffs[j][k]] once per row.  The inner loop
// then collapses to one logLUT lookup (for sym) plus 8 expLUT lookups,
// with no function calls and no sync.Once probe.
//
// We replicate logLUT / expLUT locally because they are package-private
// inside klauspost.  The tables are bit-for-bit identical to klauspost's —
// `TestComputeRLCLogTabMatchesScalar` locks that in against the reference
// path.

const (
	gf16Order      = 1 << 16
	gf16Modulus    = gf16Order - 1
	gf16Polynomial = 0x1002D
)

// localLogLUT[x] = discrete log (in the Cantor basis) of x.
// localExpLUT[i] = element whose log is i.
//
// Both tables follow klauspost's Cantor-basis Leopard encoding so that
// GF16Mul(a,b) == localExpLUT[addModLUT(localLogLUT[a], localLogLUT[b])]
// for a,b != 0.
var (
	localLogLUT [gf16Order]uint16
	localExpLUT [gf16Order]uint16
)

// coeffLogSentinel marks "this coefficient is zero", in which case we must
// skip the addModLUT/expLUT lookup because there is no valid logarithm.
// 0xFFFF (= gf16Modulus, the "infinity" code that klauspost already uses
// for log-of-zero elsewhere in leopard.go) is a safe choice: it is never a
// valid log (logLUT image is [0, gf16Modulus-1]).
const coeffLogSentinel uint16 = 0xFFFF

// cantorBasis matches klauspost's leopard.go exactly. Without this, the
// tables would still be valid GF(2^16) log/exp tables, but they would not
// agree with klauspost, so GF16Mul results would differ.
var cantorBasis = [16]uint16{
	0x0001, 0xACCA, 0x3C0E, 0x163E,
	0xC582, 0xED2E, 0x914C, 0x4012,
	0x6C98, 0x10D8, 0x6A72, 0xB900,
	0xFDB8, 0xFB34, 0xFF38, 0x991E,
}

func init() {
	initLocalLUTs()
}

// initLocalLUTs reproduces klauspost's initLUTs() bit-for-bit.
func initLocalLUTs() {
	// LFSR generation into expLUT. After this, expLUT is in the "LFSR" basis.
	state := 1
	for i := range gf16Modulus {
		localExpLUT[state] = uint16(i)
		state <<= 1
		if state >= gf16Order {
			state ^= gf16Polynomial
		}
	}
	localExpLUT[0] = gf16Modulus

	// logLUT in the Cantor basis: logLUT[j+width] = logLUT[j] ^ basis[i].
	localLogLUT[0] = 0
	for i := range 16 {
		basis := cantorBasis[i]
		width := 1 << i
		for j := range width {
			localLogLUT[j+width] = localLogLUT[j] ^ basis
		}
	}

	// Translate through expLUT to rebuild logLUT in the final form.
	for i := range gf16Order {
		localLogLUT[i] = localExpLUT[localLogLUT[i]]
	}

	// Invert: expLUT[logLUT[i]] = i.
	for i := range gf16Order {
		localExpLUT[localLogLUT[i]] = uint16(i)
	}

	// Ensure expLUT[modulus] == expLUT[0] (no-op guard from klauspost).
	localExpLUT[gf16Modulus] = localExpLUT[0]
}

// rlcCoeffLog is the per-row precomputed log of every coefficient.
//
// Layout: flat [len(coeffs)*8]uint16 in AoS order matching field.GF128 — so
// entry (j*8 + k) == localLogLUT[uint16(coeffs[j][k])], with the sentinel
// coeffLogSentinel when coeffs[j][k] == 0.
//
// Size for fibre's default row (16384 symbols): 16384 * 8 * 2 = 256 KiB,
// which fits comfortably in a c6id.8xlarge's 1.25 MiB L2.
type rlcCoeffLog struct {
	log []uint16
}

// buildRLCCoeffLog runs once per row: converts every coefficient in coeffs
// into its logarithm so the per-symbol loop can skip the sym-side log
// lookup. Zero coefficients get a sentinel (coeffLogSentinel).
//
// Cost: 8*len(coeffs) table loads + a 256 KiB alloc. Dwarfed by a single
// computeRLC pass.
func buildRLCCoeffLog(coeffs []field.GF128) *rlcCoeffLog {
	out := make([]uint16, len(coeffs)*8)
	for j := range coeffs {
		off := j * 8
		c := &coeffs[j]
		for k := range 8 {
			v := uint16(c[k])
			if v == 0 {
				out[off+k] = coeffLogSentinel
			} else {
				out[off+k] = localLogLUT[v]
			}
		}
	}
	return &rlcCoeffLog{log: out}
}

// computeRLCLogTab is the fast RLC using precomputed coefficient logs.
//
// Outer loop walks the Leopard-packed row (64-byte chunks, 32 symbols per
// chunk, low/high-byte split). Inner loop XORs into 8 register-held
// accumulators using one logLUT probe per symbol plus 8 expLUT probes.
//
// A symbol whose value is 0 is skipped (XOR by 0 is a no-op).
// A coefficient whose log is coeffLogSentinel (i.e. coeff == 0) is also
// skipped because GF16Mul(x, 0) == 0.
func computeRLCLogTab(row []byte, coeffLog *rlcCoeffLog) field.GF128 {
	var r0, r1, r2, r3, r4, r5, r6, r7 uint16
	numChunks := len(row) / chunkSize
	cl := coeffLog.log
	// Local aliases so the compiler keeps them in registers / stack.
	logLUT := &localLogLUT
	expLUT := &localExpLUT

	for c := range numChunks {
		chunkOff := c * chunkSize
		coeffOff := c * 32 * 8 // 8-wide stride into coeffLog
		for j := range 32 {
			sym := uint16(row[chunkOff+32+j])<<8 | uint16(row[chunkOff+j])
			if sym == 0 {
				continue
			}
			logSym := uint32(logLUT[sym])
			base := coeffOff + j*8
			// Each slot: if coeff log is sentinel, XOR by 0 (no-op).
			// Otherwise expLUT[(logSym + coeffLog[k]) mod modulus].
			// addModLUT inlined; also hoist compare to short-circuit.
			{
				cv := cl[base]
				if cv != coeffLogSentinel {
					s := logSym + uint32(cv)
					s += s >> 16
					r0 ^= expLUT[uint16(s)]
				}
			}
			{
				cv := cl[base+1]
				if cv != coeffLogSentinel {
					s := logSym + uint32(cv)
					s += s >> 16
					r1 ^= expLUT[uint16(s)]
				}
			}
			{
				cv := cl[base+2]
				if cv != coeffLogSentinel {
					s := logSym + uint32(cv)
					s += s >> 16
					r2 ^= expLUT[uint16(s)]
				}
			}
			{
				cv := cl[base+3]
				if cv != coeffLogSentinel {
					s := logSym + uint32(cv)
					s += s >> 16
					r3 ^= expLUT[uint16(s)]
				}
			}
			{
				cv := cl[base+4]
				if cv != coeffLogSentinel {
					s := logSym + uint32(cv)
					s += s >> 16
					r4 ^= expLUT[uint16(s)]
				}
			}
			{
				cv := cl[base+5]
				if cv != coeffLogSentinel {
					s := logSym + uint32(cv)
					s += s >> 16
					r5 ^= expLUT[uint16(s)]
				}
			}
			{
				cv := cl[base+6]
				if cv != coeffLogSentinel {
					s := logSym + uint32(cv)
					s += s >> 16
					r6 ^= expLUT[uint16(s)]
				}
			}
			{
				cv := cl[base+7]
				if cv != coeffLogSentinel {
					s := logSym + uint32(cv)
					s += s >> 16
					r7 ^= expLUT[uint16(s)]
				}
			}
		}
	}
	return field.GF128{
		field.GF16(r0), field.GF16(r1), field.GF16(r2), field.GF16(r3),
		field.GF16(r4), field.GF16(r5), field.GF16(r6), field.GF16(r7),
	}
}
