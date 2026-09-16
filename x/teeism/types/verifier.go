package types

import (
	"bytes"
	"crypto/subtle"
	"time"

	"github.com/google/go-tdx-guest/abi"
	pb "github.com/google/go-tdx-guest/proto/tdx"
	"github.com/google/go-tdx-guest/verify"
)

// MaxQuoteSkewSecs is how far ahead of the attested chain head a submitted quote
// may be.
//
// Without an upper bound a quote could be replayed indefinitely. Without the
// matching lower bound (now >= attested_at) a submitter could claim a time in the
// past and revive a platform Intel has since revoked, because every certificate,
// CRL and TCB validity window is evaluated at that time.
//
// A day rather than something tighter: attested_at is a finalized block's
// timestamp, so it already trails wall clock before any work starts, and batches
// queue behind one another. Staleness is better bounded by the transition rules,
// which require the height to advance and the root to change, so an old quote
// cannot rewind a chain that has moved on.
const MaxQuoteSkewSecs = 24 * 60 * 60

// tdAttributesDebug is bit 0 of TD_ATTRIBUTES, set when the TD runs in debug
// mode. A debug TD can be inspected and single-stepped by its host, so its
// measurements prove nothing about what actually executed.
const tdAttributesDebug = 0x01

// AttestationResult is what a verified quote establishes.
type AttestationResult struct {
	Update       *AttestedUpdate
	Measurements *Measurements
}

// VerifyAttestation checks a quote and everything it must say before its payload
// may be believed.
//
// The order matters and no step may be skipped:
//
//  1. Intel's signature chain over the quote, against collateral, at block time.
//     Answers "did genuine, current hardware sign this?".
//  2. The TD is not in debug mode.
//  3. The dstack event log replays to the RTMRs in the quote, and the pinned
//     measurements match. Answers "is this our enclave?".
//  4. The payload really rides in report_data, the transition is legal, and the
//     clock is anchored to attested chain time.
//
// blockTime is the deterministic clock: every validator evaluates the same
// certificate and TCB validity windows, so every validator reaches the same
// verdict.
func VerifyAttestation(
	quote []byte,
	eventLogRaw []byte,
	collateral *Collateral,
	payload []byte,
	identity *EnclaveIdentity,
	blockTime time.Time,
) (*AttestationResult, error) {
	measured, reportData, err := VerifyQuote(quote, collateral, blockTime)
	if err != nil {
		return nil, err
	}

	// Step 3. Our verdict on the software stack, as opposed to Intel's on the
	// hardware.
	eventLog, err := ParseEventLog(eventLogRaw)
	if err != nil {
		return nil, err
	}
	if err := VerifyEnclaveIdentity(identity, measured, eventLog, ReplayEventLogs(eventLog)); err != nil {
		return nil, err
	}

	// Step 4. The payload.
	update, err := DecodeAttestedUpdate(payload)
	if err != nil {
		return nil, ErrMalformedPayload.Wrap(err.Error())
	}

	want := HashAttestedUpdate(update)
	if subtle.ConstantTimeCompare(reportData[:32], want[:]) != 1 {
		return nil, ErrPayloadNotAttested.Wrapf("report_data commits to %x, payload hashes to %x", reportData[:32], want[:])
	}
	// The enclave leaves the second half zero. Refusing anything else keeps the
	// attested payload the only thing report_data can carry.
	if !bytes.Equal(reportData[32:], make([]byte, 32)) {
		return nil, ErrPayloadNotAttested.Wrapf("report_data has unaccounted-for bytes past the payload hash: %x", reportData[32:])
	}

	if err := VerifyTransition(update.PrevState, update.NewState); err != nil {
		return nil, err
	}

	now := uint64(blockTime.Unix())
	if now < update.AttestedAt {
		return nil, ErrStaleAttestation.Wrapf("block time %d is behind the attested head %d", now, update.AttestedAt)
	}
	if now > update.AttestedAt+MaxQuoteSkewSecs {
		return nil, ErrStaleAttestation.Wrapf("block time %d is more than %ds past the attested head %d",
			now, MaxQuoteSkewSecs, update.AttestedAt)
	}
	// The attested time must also be at least as new as the state it carries, or
	// an L2 origin could pair a fresh L1 header with an arbitrarily old L2 root
	// and defeat the staleness bound above.
	if update.AttestedAt < update.NewState.Timestamp {
		return nil, ErrStaleAttestation.Wrapf("attested at %d, before the state it carries at %d",
			update.AttestedAt, update.NewState.Timestamp)
	}

	return &AttestationResult{Update: update, Measurements: measured}, nil
}

// VerifyQuote runs steps 1 and 2 on their own: Intel's verdict on the hardware,
// plus the debug-mode check.
//
// Exported so that tooling which assembles collateral can confirm a bundle
// actually verifies through the same path consensus will use, rather than
// through a near-copy of it.
func VerifyQuote(quote []byte, collateral *Collateral, blockTime time.Time) (*Measurements, []byte, error) {
	if err := collateral.Validate(); err != nil {
		return nil, nil, err
	}

	parsed, err := abi.QuoteToProto(quote)
	if err != nil {
		return nil, nil, ErrInvalidQuote.Wrapf("could not parse quote: %s", err.Error())
	}
	quoteV4, ok := parsed.(*pb.QuoteV4)
	if !ok {
		return nil, nil, ErrInvalidQuote.Wrap("only TDX quote v4 is supported")
	}

	// Signature chain, PCK certificate chain, CRLs and TCB status, all evaluated
	// against collateral carried in the transaction at block time.
	if err := verify.TdxQuote(quoteV4, &verify.Options{
		CheckRevocations: true,
		GetCollateral:    true,
		Getter:           offlineGetter{collateral: collateral},
		Now:              blockTime,
	}); err != nil {
		return nil, nil, ErrInvalidQuote.Wrapf("dcap verification failed: %s", err.Error())
	}

	body := quoteV4.GetTdQuoteBody()
	if body == nil {
		return nil, nil, ErrInvalidQuote.Wrap("quote has no TD report body")
	}

	// A debug TD is transparent to its host, so its measurements mean nothing.
	// go-tdx-guest checks the signature chain but takes no view on this.
	attrs := body.GetTdAttributes()
	if len(attrs) != 8 {
		return nil, nil, ErrInvalidQuote.Wrapf("td_attributes must be 8 bytes, got %d", len(attrs))
	}
	if attrs[0]&tdAttributesDebug != 0 {
		return nil, nil, ErrInvalidQuote.Wrap("td is in debug mode")
	}

	measured, err := MeasurementsFrom(body.GetMrTd(), body.GetRtmrs())
	if err != nil {
		return nil, nil, ErrInvalidQuote.Wrap(err.Error())
	}

	reportData := body.GetReportData()
	if len(reportData) != 64 {
		return nil, nil, ErrInvalidQuote.Wrapf("report_data must be 64 bytes, got %d", len(reportData))
	}
	return measured, reportData, nil
}
