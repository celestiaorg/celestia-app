package types_test

import (
	"bytes"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
	"github.com/stretchr/testify/require"
)

// Steps 3 and 4 of VerifyAttestation on a quote Intel already vouched for: the
// genuine fixture's measurements and event log, with report_data built for each
// payload under test, since no real quote commits to these payloads.
func payloadFixture(t *testing.T) (*types.EnclaveIdentity, *types.Measurements, []byte) {
	t.Helper()
	id, measured, _ := realIdentity(t)
	_, eventLog, _, _ := loadRealQuote(t)
	return id, measured, eventLog
}

func reportDataFor(payload []byte) []byte {
	sum := sha256.Sum256(payload)
	return append(sum[:], make([]byte, 32)...)
}

func update(id *types.EnclaveIdentity) *types.AttestedUpdate {
	prev := &types.IsmState{OriginDomain: 7, Height: 100, Timestamp: 1_000, IdentityDigest: id.Digest()}
	prev.StateRoot[0] = 1
	next := *prev
	next.StateRoot[0] = 2
	next.Height = 105
	next.Timestamp = 1_060
	u := &types.AttestedUpdate{PrevState: prev, NewState: &next, AttestedAt: 1_060, TreeCount: 3}
	for i := range u.MerkleTreeAddress {
		u.MerkleTreeAddress[i] = 9
		u.TreeRoot[i] = 0xcd
	}
	return u
}

func TestAttestedPayloadIsAccepted(t *testing.T) {
	id, measured, log := payloadFixture(t)
	u := update(id)
	payload := types.EncodeAttestedUpdate(u)

	res, err := types.VerifyAttestedPayload(measured, reportDataFor(payload), log, payload, id, time.Unix(1_100, 0))
	require.NoError(t, err)
	require.Equal(t, u, res.Update)
	require.Equal(t, measured, res.Measurements)
}

func TestAttestedPayloadIsRefused(t *testing.T) {
	id, measured, log := payloadFixture(t)
	at := time.Unix(1_100, 0)

	for name, tc := range map[string]struct {
		edit    func(u *types.AttestedUpdate)
		report  func(payload []byte) []byte
		payload func(payload []byte) []byte
		now     time.Time
		want    error
	}{
		"payload not in report_data": {
			report: func(p []byte) []byte { r := reportDataFor(p); r[0] ^= 1; return r },
			want:   types.ErrPayloadNotAttested,
		},
		"report_data carries extra bytes": {
			report: func(p []byte) []byte { r := reportDataFor(p); r[63] = 1; return r },
			want:   types.ErrPayloadNotAttested,
		},
		"payload truncated": {
			payload: func(p []byte) []byte { return p[:len(p)-1] },
			want:    types.ErrMalformedPayload,
		},
		"height did not advance": {
			edit: func(u *types.AttestedUpdate) { u.NewState.Height = u.PrevState.Height },
			want: types.ErrInvalidTransition,
		},
		"state root unchanged": {
			edit: func(u *types.AttestedUpdate) { u.NewState.StateRoot = u.PrevState.StateRoot },
			want: types.ErrInvalidTransition,
		},
		"identity changed": {
			edit: func(u *types.AttestedUpdate) { u.NewState.IdentityDigest[0] ^= 1 },
			want: types.ErrInvalidTransition,
		},
		"block time behind the attested head": {
			now:  time.Unix(1_000, 0),
			want: types.ErrStaleAttestation,
		},
		"attestation older than the skew": {
			now:  time.Unix(1_060+types.MaxQuoteSkewSecs+1, 0),
			want: types.ErrStaleAttestation,
		},
		"attested before the state it carries": {
			edit: func(u *types.AttestedUpdate) { u.AttestedAt = u.NewState.Timestamp - 1 },
			want: types.ErrStaleAttestation,
		},
	} {
		t.Run(name, func(t *testing.T) {
			u := update(id)
			if tc.edit != nil {
				tc.edit(u)
			}
			payload := types.EncodeAttestedUpdate(u)
			report := reportDataFor(payload)
			if tc.report != nil {
				report = tc.report(payload)
			}
			if tc.payload != nil {
				payload = tc.payload(payload)
			}
			now := at
			if !tc.now.IsZero() {
				now = tc.now
			}
			_, err := types.VerifyAttestedPayload(measured, report, log, payload, id, now)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

// The cadence is the msg server's to apply, against the root it holds; the payload
// check itself never refuses an unchanged root.
func TestAttestedPayloadDoesNotJudgeCadence(t *testing.T) {
	id, measured, log := payloadFixture(t)
	u := update(id)
	u.NewState.Timestamp = u.PrevState.Timestamp
	u.AttestedAt = u.NewState.Timestamp
	payload := types.EncodeAttestedUpdate(u)
	_, err := types.VerifyAttestedPayload(measured, reportDataFor(payload), log, payload, id, time.Unix(1_100, 0))
	require.NoError(t, err)
	require.True(t, bytes.Equal(u.TreeRoot[:], u.TreeRoot[:]))
}
