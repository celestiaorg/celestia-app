package types_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/teeism/types"
	"github.com/stretchr/testify/require"
)

// A genuine TDX quote from a dstack CVM, captured with the Intel collateral that
// was live at the time.
//
// Everything above this file can be exercised with synthetic data; DCAP
// verification cannot. Only a real quote proves that the signature chain, the PCK
// certificate parsing, the TCB lookup and the CRL checks all agree with what
// Intel actually serves, and that the collateral the relayer ships is enough to
// verify one without touching the network.
type realQuoteBundle struct {
	VerifiedAt int64  `json:"verified_at"`
	Quote      string `json:"quote"`
	EventLog   string `json:"event_log"`
	Collateral struct {
		PckCrl                string `json:"pck_crl"`
		PckCrlIssuerChain     string `json:"pck_crl_issuer_chain"`
		TcbInfo               string `json:"tcb_info"`
		TcbInfoIssuerChain    string `json:"tcb_info_issuer_chain"`
		QeIdentity            string `json:"qe_identity"`
		QeIdentityIssuerChain string `json:"qe_identity_issuer_chain"`
		RootCaCrl             string `json:"root_ca_crl"`
	} `json:"collateral"`
}

func loadRealQuote(t *testing.T) (quote []byte, eventLog []byte, collateral *types.Collateral, at time.Time) {
	t.Helper()
	raw, err := os.ReadFile("../internal/testdata/real_quote.json")
	require.NoError(t, err)

	var b realQuoteBundle
	require.NoError(t, json.Unmarshal(raw, &b))

	dec := func(s string) []byte {
		v, err := hex.DecodeString(trim0x(s))
		require.NoError(t, err)
		return v
	}

	return dec(b.Quote), []byte(b.EventLog), &types.Collateral{
		PckCrl:                dec(b.Collateral.PckCrl),
		PckCrlIssuerChain:     dec(b.Collateral.PckCrlIssuerChain),
		TcbInfo:               dec(b.Collateral.TcbInfo),
		TcbInfoIssuerChain:    dec(b.Collateral.TcbInfoIssuerChain),
		QeIdentity:            dec(b.Collateral.QeIdentity),
		QeIdentityIssuerChain: dec(b.Collateral.QeIdentityIssuerChain),
		RootCaCrl:             dec(b.Collateral.RootCaCrl),
		// The clock is pinned by the caller, so validity windows do not drift.
	}, time.Unix(b.VerifiedAt, 0)
}

func trim0x(s string) string {
	if len(s) >= 2 && s[:2] == "0x" {
		return s[2:]
	}
	return s
}

func TestVerifyRealQuoteOffline(t *testing.T) {
	quote, eventLog, collateral, at := loadRealQuote(t)

	measured, reportData, err := types.VerifyQuote(quote, collateral, at)
	require.NoError(t, err, "a genuine quote must verify from carried collateral alone")
	require.Len(t, reportData, 64)

	// The event log is only believable once it replays to the registers the
	// hardware signed.
	log, err := types.ParseEventLog(eventLog)
	require.NoError(t, err)
	require.Equal(t, measured.Rtmrs, types.ReplayEventLogs(log),
		"event log does not replay to the RTMRs in the quote")
}

func TestRealQuoteYieldsAPinnableIdentity(t *testing.T) {
	quote, eventLog, collateral, at := loadRealQuote(t)

	measured, _, err := types.VerifyQuote(quote, collateral, at)
	require.NoError(t, err)

	log, err := types.ParseEventLog(eventLog)
	require.NoError(t, err)

	id := &types.EnclaveIdentity{MrTd: measured.MrTd[:]}
	for _, f := range []struct {
		name string
		dst  *[]byte
	}{
		{types.EventOsImageHash, &id.OsImageHash},
		{types.EventComposeHash, &id.ComposeHash},
		{types.EventMrKms, &id.MrKms},
		{types.EventKeyProvider, &id.KeyProvider},
	} {
		v, ok := types.GetEventValue(log, f.name)
		require.True(t, ok, "event %q must be readable and self-consistent", f.name)
		*f.dst = v
	}
	require.NoError(t, id.Validate())

	// An identity built from a quote must be the identity that quote satisfies.
	require.NoError(t, types.VerifyEnclaveIdentity(id, measured, log, types.ReplayEventLogs(log)))
}

func TestRealQuoteRejectsTampering(t *testing.T) {
	quote, eventLog, collateral, at := loadRealQuote(t)

	// The fields an ISM actually relies on. Each is inside the region Intel's
	// signature covers, so a single flipped bit has to be caught.
	for name, off := range map[string]int{
		"mr_td":          232,
		"report_data":    568,
		"pck cert chain": len(quote) - 256,
	} {
		t.Run("flipped byte in "+name, func(t *testing.T) {
			bad := append([]byte{}, quote...)
			bad[off] ^= 0x01
			_, _, err := types.VerifyQuote(bad, collateral, at)
			require.Error(t, err)
		})
	}

	t.Run("trailing padding is not covered", func(t *testing.T) {
		// dstack returns the quote in a fixed-size buffer, so it ends in zero
		// padding that sits outside the parsed structure and outside Intel's
		// signature. Flipping it changes nothing.
		//
		// Recorded rather than asserted away, because it means two byte strings
		// can be the same quote. Nothing here keys off the raw quote bytes:
		// authorized messages come from the attested payload, and replay is
		// prevented by the state chain, so a non-canonical encoding buys an
		// attacker nothing.
		bad := append([]byte{}, quote...)
		bad[len(bad)-1] ^= 0x01
		_, _, err := types.VerifyQuote(bad, collateral, at)
		require.NoError(t, err)
	})

	t.Run("missing collateral", func(t *testing.T) {
		bad := *collateral
		bad.TcbInfo = nil
		_, _, err := types.VerifyQuote(quote, &bad, at)
		require.Error(t, err)
	})

	t.Run("truncated tcb info", func(t *testing.T) {
		bad := *collateral
		bad.TcbInfo = bad.TcbInfo[:len(bad.TcbInfo)/2]
		_, _, err := types.VerifyQuote(quote, &bad, at)
		require.Error(t, err)
	})

	t.Run("clock long before the collateral was issued", func(t *testing.T) {
		// Certificates and CRLs have validity windows, so a clock outside them
		// must fail. This is why block time drives verification rather than a
		// value the submitter chooses.
		_, _, err := types.VerifyQuote(quote, collateral, at.AddDate(-5, 0, 0))
		require.Error(t, err)
	})

	t.Run("identity pinned to a different enclave", func(t *testing.T) {
		measured, _, err := types.VerifyQuote(quote, collateral, at)
		require.NoError(t, err)
		log, err := types.ParseEventLog(eventLog)
		require.NoError(t, err)

		compose, ok := types.GetEventValue(log, types.EventComposeHash)
		require.True(t, ok)
		osImage, _ := types.GetEventValue(log, types.EventOsImageHash)
		mrKms, _ := types.GetEventValue(log, types.EventMrKms)
		keyProvider, _ := types.GetEventValue(log, types.EventKeyProvider)

		other := &types.EnclaveIdentity{
			MrTd:        append([]byte{}, measured.MrTd[:]...),
			OsImageHash: osImage,
			ComposeHash: append([]byte{}, compose...),
			MrKms:       mrKms,
			KeyProvider: keyProvider,
		}
		other.ComposeHash[0] ^= 0x01

		err = types.VerifyEnclaveIdentity(other, measured, log, types.ReplayEventLogs(log))
		require.ErrorIs(t, err, types.ErrIdentityMismatch)
	})
}
