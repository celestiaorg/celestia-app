package types_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
)

// The work one MsgSubmitAttestation costs every validator, measured on the real
// fixture. DefaultAttestationVerifyCost and the per-block weight in
// app/msg_count.go are derived from these numbers; rerun them when go-tdx-guest
// or the identity check changes:
//
//	go test ./x/teeism/types/ -run XXX -bench 'BenchmarkVerify' -benchtime 20x
func loadBenchQuote(b *testing.B) (quote []byte, eventLog []byte, collateral *types.Collateral, at time.Time) {
	b.Helper()
	raw, err := os.ReadFile("../internal/testdata/real_quote.json")
	if err != nil {
		b.Fatal(err)
	}
	var bundle realQuoteBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		b.Fatal(err)
	}
	dec := func(s string) []byte {
		v, err := hex.DecodeString(trim0x(s))
		if err != nil {
			b.Fatal(err)
		}
		return v
	}
	return dec(bundle.Quote), []byte(bundle.EventLog), &types.Collateral{
		PckCrl:                dec(bundle.Collateral.PckCrl),
		PckCrlIssuerChain:     dec(bundle.Collateral.PckCrlIssuerChain),
		TcbInfo:               dec(bundle.Collateral.TcbInfo),
		TcbInfoIssuerChain:    dec(bundle.Collateral.TcbInfoIssuerChain),
		QeIdentity:            dec(bundle.Collateral.QeIdentity),
		QeIdentityIssuerChain: dec(bundle.Collateral.QeIdentityIssuerChain),
		RootCaCrl:             dec(bundle.Collateral.RootCaCrl),
	}, time.Unix(bundle.VerifiedAt, 0)
}

// Intel's side: signature chain, CRLs, TCB status. The bulk of an attestation.
func BenchmarkVerifyQuote(b *testing.B) {
	quote, _, collateral, at := loadBenchQuote(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := types.VerifyQuote(quote, collateral, at); err != nil {
			b.Fatal(err)
		}
	}
}

// Our side: parse the event log, replay it, match it against the pinned set.
func benchIdentities(b *testing.B, n int) {
	quote, eventLog, collateral, at := loadBenchQuote(b)
	measured, _, err := types.VerifyQuote(quote, collateral, at)
	if err != nil {
		b.Fatal(err)
	}
	log, err := types.ParseEventLog(eventLog)
	if err != nil {
		b.Fatal(err)
	}
	id := &types.EnclaveIdentity{MrTd: measured.MrTd[:], Rtmr0: measured.Rtmrs[0][:], Rtmr1: measured.Rtmrs[1][:], Rtmr2: measured.Rtmrs[2][:]}
	for _, f := range []struct {
		name string
		dst  *[]byte
	}{{types.EventOsImageHash, &id.OsImageHash}, {types.EventComposeHash, &id.ComposeHash}, {types.EventKeyProvider, &id.KeyProvider}} {
		v, ok := types.GetEventValue(log, f.name)
		if !ok {
			b.Fatal(f.name)
		}
		*f.dst = v
	}
	// n-1 decoys that differ in mr_td, tried and rejected before the match.
	ids := make([]*types.EnclaveIdentity, 0, n)
	for i := 1; i < n; i++ {
		decoy := *id
		decoy.MrTd = append([]byte(nil), id.MrTd...)
		decoy.MrTd[0] ^= byte(i)
		ids = append(ids, &decoy)
	}
	ids = append(ids, id)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		log, err := types.ParseEventLog(eventLog)
		if err != nil {
			b.Fatal(err)
		}
		if err := types.VerifyEnclaveIdentities(ids, measured, log, types.ReplayEventLogs(log)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyIdentities1(b *testing.B) { benchIdentities(b, 1) }
func BenchmarkVerifyIdentities8(b *testing.B) { benchIdentities(b, 8) }
