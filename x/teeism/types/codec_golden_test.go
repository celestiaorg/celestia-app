package types_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/x/teeism/types"
	"github.com/stretchr/testify/require"
)

// The enclave encodes these formats in Rust and this module decodes them in Go.
// A single byte of drift rejects every attestation the bridge submits, and it
// would do so at runtime on a live chain rather than here. The fixture is
// produced by tee-circuit's `emit_vectors` example, which calls the same
// functions the enclave and its circuits call.
type rustVectors struct {
	IsmState struct {
		StateRoot      string `json:"state_root"`
		OriginDomain   uint32 `json:"origin_domain"`
		Height         uint64 `json:"height"`
		Timestamp      uint64 `json:"timestamp"`
		LcStoreCommit  string `json:"lc_store_commit"`
		IdentityDigest string `json:"identity_digest"`
		Encoded        string `json:"encoded"`
	} `json:"ism_state"`
	AttestedUpdate struct {
		Encoded           string   `json:"encoded"`
		Hash              string   `json:"hash"`
		MerkleTreeAddress string   `json:"merkle_tree_address"`
		AttestedAt        uint64   `json:"attested_at"`
		MessageIDs        []string `json:"message_ids"`
	} `json:"attested_update"`
	AttestedUpdateEmpty struct {
		Encoded string `json:"encoded"`
		Hash    string `json:"hash"`
	} `json:"attested_update_empty"`
	Identity struct {
		MrTd        string `json:"mr_td"`
		OsImageHash string `json:"os_image_hash"`
		ComposeHash string `json:"compose_hash"`
		MrKms       string `json:"mr_kms"`
		KeyProvider string `json:"key_provider"`
		Digest      string `json:"digest"`
	} `json:"identity"`
	EventLog struct {
		JSON  string   `json:"json"`
		Rtmrs []string `json:"rtmrs"`
	} `json:"event_log"`
	MaxQuoteSkewSecs uint64 `json:"max_quote_skew_secs"`
	IsmStateBytes    int    `json:"ism_state_bytes"`
}

func loadVectors(t *testing.T) rustVectors {
	t.Helper()
	raw, err := os.ReadFile("../internal/testdata/rust_vectors.json")
	require.NoError(t, err)
	var v rustVectors
	require.NoError(t, json.Unmarshal(raw, &v))
	return v
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

func TestIsmStateMatchesRustEncoder(t *testing.T) {
	v := loadVectors(t)
	require.Equal(t, types.IsmStateBytes, v.IsmStateBytes, "state width disagrees across languages")

	want := unhex(t, v.IsmState.Encoded)

	got := types.EncodeIsmState(&types.IsmState{
		StateRoot:      [32]byte(unhex(t, v.IsmState.StateRoot)),
		OriginDomain:   v.IsmState.OriginDomain,
		Height:         v.IsmState.Height,
		Timestamp:      v.IsmState.Timestamp,
		LcStoreCommit:  [32]byte(unhex(t, v.IsmState.LcStoreCommit)),
		IdentityDigest: [32]byte(unhex(t, v.IsmState.IdentityDigest)),
	})
	require.Equal(t, want, got, "Go encoder disagrees with the Rust encoder")

	// And the other direction: what Rust wrote must decode to the same fields.
	back, err := types.DecodeIsmState(want)
	require.NoError(t, err)
	require.Equal(t, v.IsmState.OriginDomain, back.OriginDomain)
	require.Equal(t, v.IsmState.Height, back.Height)
	require.Equal(t, v.IsmState.Timestamp, back.Timestamp)
	require.Equal(t, unhex(t, v.IsmState.StateRoot), back.StateRoot[:])
	require.Equal(t, unhex(t, v.IsmState.LcStoreCommit), back.LcStoreCommit[:])
	require.Equal(t, unhex(t, v.IsmState.IdentityDigest), back.IdentityDigest[:])
}

func TestAttestedUpdateMatchesRustEncoder(t *testing.T) {
	v := loadVectors(t)

	for _, tc := range []struct{ name, encoded, hash string }{
		{"with messages", v.AttestedUpdate.Encoded, v.AttestedUpdate.Hash},
		{"empty batch", v.AttestedUpdateEmpty.Encoded, v.AttestedUpdateEmpty.Hash},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := unhex(t, tc.encoded)

			update, err := types.DecodeAttestedUpdate(raw)
			require.NoError(t, err)

			// Re-encoding must reproduce the exact bytes, or the payload hash the
			// enclave signed would not be the payload hash we check.
			require.Equal(t, raw, types.EncodeAttestedUpdate(update))

			digest := types.HashAttestedUpdate(update)
			require.Equal(t, unhex(t, tc.hash), digest[:],
				"report_data would not match what the enclave committed")
		})
	}

	update, err := types.DecodeAttestedUpdate(unhex(t, v.AttestedUpdate.Encoded))
	require.NoError(t, err)
	require.Equal(t, v.AttestedUpdate.AttestedAt, update.AttestedAt)
	require.Equal(t, unhex(t, v.AttestedUpdate.MerkleTreeAddress), update.MerkleTreeAddress[:])
	require.Len(t, update.MessageIDs, len(v.AttestedUpdate.MessageIDs))
	for i, want := range v.AttestedUpdate.MessageIDs {
		require.Equal(t, unhex(t, want), update.MessageIDs[i][:])
	}
}

func TestAttestedUpdateRejectsMalformed(t *testing.T) {
	v := loadVectors(t)
	raw := unhex(t, v.AttestedUpdate.Encoded)

	t.Run("truncated", func(t *testing.T) {
		_, err := types.DecodeAttestedUpdate(raw[:len(raw)-1])
		require.Error(t, err)
	})

	t.Run("trailing bytes", func(t *testing.T) {
		// Trailing bytes would let two different encodings hash to two different
		// values while decoding to the same update, so they are refused.
		_, err := types.DecodeAttestedUpdate(append(append([]byte{}, raw...), 0x00))
		require.Error(t, err)
	})

	t.Run("count larger than the buffer", func(t *testing.T) {
		bad := append([]byte{}, raw...)
		// The count sits immediately before the ids.
		off := 2*types.IsmStateBytes + 32 + 8
		for i := 0; i < 8; i++ {
			bad[off+i] = 0xFF
		}
		_, err := types.DecodeAttestedUpdate(bad)
		require.Error(t, err)
	})
}

func TestIdentityDigestMatchesRust(t *testing.T) {
	v := loadVectors(t)

	id := &types.EnclaveIdentity{
		MrTd:        unhex(t, v.Identity.MrTd),
		OsImageHash: unhex(t, v.Identity.OsImageHash),
		ComposeHash: unhex(t, v.Identity.ComposeHash),
		MrKms:       unhex(t, v.Identity.MrKms),
		KeyProvider: unhex(t, v.Identity.KeyProvider),
	}
	require.NoError(t, id.Validate())

	digest := id.Digest()
	require.Equal(t, unhex(t, v.Identity.Digest), digest[:],
		"an ISM would name a different enclave than the one the circuit pins")

	// The digest must actually depend on every pinned field, or pinning is
	// decorative.
	for name, mutate := range map[string]func(*types.EnclaveIdentity){
		"mr_td":         func(i *types.EnclaveIdentity) { i.MrTd[0] ^= 1 },
		"os_image_hash": func(i *types.EnclaveIdentity) { i.OsImageHash[0] ^= 1 },
		"compose_hash":  func(i *types.EnclaveIdentity) { i.ComposeHash[0] ^= 1 },
		"mr_kms":        func(i *types.EnclaveIdentity) { i.MrKms[0] ^= 1 },
		"key_provider":  func(i *types.EnclaveIdentity) { i.KeyProvider[0] ^= 1 },
	} {
		t.Run("digest depends on "+name, func(t *testing.T) {
			other := &types.EnclaveIdentity{
				MrTd:        append([]byte{}, id.MrTd...),
				OsImageHash: append([]byte{}, id.OsImageHash...),
				ComposeHash: append([]byte{}, id.ComposeHash...),
				MrKms:       append([]byte{}, id.MrKms...),
				KeyProvider: append([]byte{}, id.KeyProvider...),
			}
			mutate(other)
			require.NotEqual(t, digest, other.Digest())
		})
	}
}

func TestEventLogReplayMatchesRust(t *testing.T) {
	v := loadVectors(t)

	log, err := types.ParseEventLog([]byte(v.EventLog.JSON))
	require.NoError(t, err)
	require.NotEmpty(t, log)

	rtmrs := types.ReplayEventLogs(log)
	require.Len(t, v.EventLog.Rtmrs, types.RtmrCount)
	for i, want := range v.EventLog.Rtmrs {
		require.Equal(t, unhex(t, want), rtmrs[i][:], "rtmr%d replay disagrees with Rust", i)
	}
}

func TestGetEventValueRejectsRelabelledEntries(t *testing.T) {
	v := loadVectors(t)
	log, err := types.ParseEventLog([]byte(v.EventLog.JSON))
	require.NoError(t, err)

	got, ok := types.GetEventValue(log, types.EventComposeHash)
	require.True(t, ok)
	require.Equal(t, unhex(t, v.Identity.ComposeHash), got)

	t.Run("relabelled entry", func(t *testing.T) {
		// Keep every genuine digest, so the RTMR replay still matches, and merely
		// rename an entry to impersonate a pinned key. Reading it must fail.
		tampered := append([]types.EventLog{}, log...)
		for i := range tampered {
			if tampered[i].Event == "instance-id" {
				tampered[i].Event = types.EventComposeHash
			}
		}
		_, ok := types.GetEventValue(tampered, types.EventComposeHash)
		require.False(t, ok, "a duplicated key must not resolve")
	})

	t.Run("payload swapped under a genuine digest", func(t *testing.T) {
		tampered := append([]types.EventLog{}, log...)
		for i := range tampered {
			if tampered[i].Event == types.EventComposeHash {
				tampered[i].EventPayload = []byte("not the pinned image")
			}
		}
		_, ok := types.GetEventValue(tampered, types.EventComposeHash)
		require.False(t, ok, "digest no longer commits to the payload")
	})
}

func TestMaxQuoteSkewMatchesRust(t *testing.T) {
	v := loadVectors(t)
	require.Equal(t, v.MaxQuoteSkewSecs, uint64(types.MaxQuoteSkewSecs))
}
