package types

import (
	"encoding/hex"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
)

const (
	// ModuleName defines the module name.
	ModuleName = "teeism"

	// StoreKey defines the primary module store key.
	StoreKey = ModuleName

	// MaxPaginationLimit is the maximum number of items returned in a paginated query.
	MaxPaginationLimit = 100

	// MaxQuoteBytes caps a submitted TDX quote. A real dstack quote is around
	// 5 KiB; the headroom covers longer PCK certificate chains.
	MaxQuoteBytes = 32 * 1024

	// MaxEventLogBytes caps the dstack event log. A real one is about 5 KiB; every
	// byte of an IMR 3 entry is hashed up to three times, so the cap bounds that
	// work, and DefaultAttestationVerifyCost prices it at the cap.
	MaxEventLogBytes = 64 * 1024

	// MaxEventLogEntries caps the entries of a dstack event log. A real one has
	// about 30; each costs a replay hash and, in IMR 3, a self-commitment check.
	MaxEventLogEntries = 256

	// MaxCollateralFieldBytes caps any single Intel PCS artifact.
	MaxCollateralFieldBytes = 256 * 1024

	// DefaultAttestationVerifyCost is the gas metered for one attestation, charged
	// before verifying so a submission that fails still pays for the work.
	//
	// Priced like appconsts.GasPerISMSignature, at about 4.7 ns per gas, from
	// BenchmarkVerifyQuote and BenchmarkVerifyIdentities (verifier_bench_test.go):
	// Intel's side (certificate chain, CRLs, TCB status, several P-256 checks)
	// measured 1.4 ms on an M-series core in pure Go, about 300k gas; parsing,
	// replaying and self-checking an event log at MaxEventLogBytes and
	// MaxEventLogEntries is under 1 ms more. The sum, rounded up. Rerun the
	// benchmarks when go-tdx-guest or the identity check changes.
	DefaultAttestationVerifyCost = 500_000

	// MaxCollateralAgeSecs bounds how old the TCB info, QE identity and PCK CRL in
	// a submission may be at block time. Intel reissues all three on request,
	// the relayer fetches them for every attestation, and their own validity
	// runs 30 days; this cuts the time a platform Intel has since revoked or
	// downgraded can keep attesting with the collateral from before.
	MaxCollateralAgeSecs = 14 * 24 * 60 * 60

	// MaxCollateralRegressionSecs is how much older than the newest collateral an
	// ism has accepted a submission's may be. Honest collateral only gets newer,
	// so a revoked platform cannot keep submitting the collateral from before its
	// revocation once any newer attestation has landed; the slack absorbs Intel
	// serving an artifact from a cache behind the one it served last.
	MaxCollateralRegressionSecs = 24 * 60 * 60
)

var (
	IsmsKeyPrefix = collections.NewPrefix(0)
	// Prefix 1 held the authorized message ids when attestations named them.
	RootsKeyPrefix = collections.NewPrefix(2)
)

// EncodeHex encodes a byte slice as a 0x prefixed hexadecimal string.
func EncodeHex(bz []byte) string {
	return fmt.Sprintf("0x%s", hex.EncodeToString(bz))
}

// DecodeHex decodes a 0x prefixed hexadecimal string.
func DecodeHex(s string) ([]byte, error) {
	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}
