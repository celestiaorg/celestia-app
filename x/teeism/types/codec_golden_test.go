package types_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
	"github.com/stretchr/testify/require"
)

// The enclave encodes these formats in Rust and this module decodes them in Go.
// A single byte of drift rejects every attestation the bridge submits, and it
// would do so at runtime on a live chain rather than here. The state, identity
// and event-log fixture is produced by the Rust encoder; the attested update
// vectors are hardcoded below.
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
	Identity struct {
		MrTd        string `json:"mr_td"`
		Rtmr0       string `json:"rtmr0"`
		Rtmr1       string `json:"rtmr1"`
		Rtmr2       string `json:"rtmr2"`
		OsImageHash string `json:"os_image_hash"`
		ComposeHash string `json:"compose_hash"`
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

// The attested update as `tee-node`'s encoder writes it (crates/tee-node/tests/attest_request.rs,
// `write_test_vectors`), with the sha256 the enclave puts in report_data. Hardcoded: a drift here
// is a drift in what every attestation hashes to.
var rustAttestedUpdates = []struct {
	name, encoded, hash string
	attestedAt          uint64
	treeCount           uint32
}{
	{
		name: "filled",
		encoded: "0101010101010101010101010101010101010101010101010101010101010101" +
			"01020304000000000000000a00000000000003e8" +
			"0202020202020202020202020202020202020202020202020202020202020202" +
			"0303030303030303030303030303030303030303030303030303030303030303" +
			"0404040404040404040404040404040404040404040404040404040404040404" +
			"01020304000000000000001400000000000007d0" +
			"0505050505050505050505050505050505050505050505050505050505050505" +
			"0606060606060606060606060606060606060606060606060606060606060606" +
			"abababababababababababababababababababababababababababababababab" +
			"0000000000000834" +
			"cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd" +
			"0a0b0c0d",
		hash:       "442db67438e638c66061b8562ea50cdaee582e2638adb068cba8695edc0beb49",
		attestedAt: 2100,
		treeCount:  0x0a0b0c0d,
	},
	{
		name: "empty tree",
		encoded: "1010101010101010101010101010101010101010101010101010101010101010" +
			"010203040000000000000005000000006553f100" +
			"1111111111111111111111111111111111111111111111111111111111111111" +
			"1212121212121212121212121212121212121212121212121212121212121212" +
			"2020202020202020202020202020202020202020202020202020202020202020" +
			"010203040000000000000006000000006553f10c" +
			"2121212121212121212121212121212121212121212121212121212121212121" +
			"2222222222222222222222222222222222222222222222222222222222222222" +
			"0000000000000000000000000000000000000000000000000000000000000000" +
			"000000006553f10c" +
			"0000000000000000000000000000000000000000000000000000000000000000" +
			"00000000",
		hash:       "d31a3b8dcceb113390e9d597694503ddcafaa5c33e0de88860a4960e0c8cdc5d",
		attestedAt: 1700000012,
		treeCount:  0,
	},
}

func TestAttestedUpdateMatchesRustEncoder(t *testing.T) {
	for _, tc := range rustAttestedUpdates {
		t.Run(tc.name, func(t *testing.T) {
			raw := unhex(t, tc.encoded)
			require.Len(t, raw, types.AttestedUpdateBytes)

			update, err := types.DecodeAttestedUpdate(raw)
			require.NoError(t, err)
			require.Equal(t, tc.attestedAt, update.AttestedAt)
			require.Equal(t, tc.treeCount, update.TreeCount)

			// Re-encoding must reproduce the exact bytes, or the payload hash the
			// enclave signed would not be the payload hash we check.
			require.Equal(t, raw, types.EncodeAttestedUpdate(update))

			digest := types.HashAttestedUpdate(update)
			require.Equal(t, unhex(t, tc.hash), digest[:],
				"report_data would not match what the enclave committed")
		})
	}

	update, err := types.DecodeAttestedUpdate(unhex(t, rustAttestedUpdates[0].encoded))
	require.NoError(t, err)
	require.Equal(t, bytes.Repeat([]byte{0xab}, 32), update.MerkleTreeAddress[:])
	require.Equal(t, bytes.Repeat([]byte{0xcd}, 32), update.TreeRoot[:])
	require.Equal(t, uint64(20), update.NewState.Height)
	require.Equal(t, uint64(2000), update.NewState.Timestamp)
}

func TestAttestedUpdateRejectsMalformed(t *testing.T) {
	raw := unhex(t, rustAttestedUpdates[0].encoded)

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

	t.Run("the previous layout", func(t *testing.T) {
		// It ended in count(8) ids(32 * count), so it was 280 + 32k bytes: never 308.
		for k := range 4 {
			_, err := types.DecodeAttestedUpdate(make([]byte, 280+32*k))
			require.Error(t, err, "k=%d", k)
		}
	})
}

// A message's delivery metadata, and the tree it is against, as the Rust side
// builds them (root_vectors.json comes from the same `write_test_vectors`).
type rootVectors struct {
	Tree struct {
		Leaves []string `json:"leaves"`
		Root   string   `json:"root_hex"`
		Count  uint32   `json:"count"`
		Proofs []struct {
			Index uint32   `json:"index"`
			Proof []string `json:"proof"`
		} `json:"proofs"`
	} `json:"tree"`
}

func loadRootVectors(t *testing.T) rootVectors {
	t.Helper()
	raw, err := os.ReadFile("../internal/testdata/root_vectors.json")
	require.NoError(t, err)
	var v rootVectors
	require.NoError(t, json.Unmarshal(raw, &v))
	return v
}

func unhex0x(t *testing.T, s string) []byte {
	t.Helper()
	return unhex(t, strings.TrimPrefix(s, "0x"))
}

func metadataFor(t *testing.T, v rootVectors, index int) *types.DeliveryMetadata {
	t.Helper()
	m := &types.DeliveryMetadata{Index: v.Tree.Proofs[index].Index}
	copy(m.Root[:], unhex0x(t, v.Tree.Root))
	for i, p := range v.Tree.Proofs[index].Proof {
		copy(m.Proof[i][:], unhex0x(t, p))
	}
	return m
}

func TestDeliveryMetadataFoldsToTheRustRoot(t *testing.T) {
	v := loadRootVectors(t)
	require.Len(t, v.Tree.Leaves, int(v.Tree.Count))
	for i, leafHex := range v.Tree.Leaves {
		var leaf [32]byte
		copy(leaf[:], unhex0x(t, leafHex))
		m := metadataFor(t, v, i)
		require.True(t, m.Proves(leaf), "leaf %d", i)

		// The encoding is what a relayer puts on the wire.
		raw := types.EncodeDeliveryMetadata(m)
		require.Len(t, raw, types.DeliveryMetadataBytes)
		back, err := types.DecodeDeliveryMetadata(raw)
		require.NoError(t, err)
		require.Equal(t, m, back)

		var other [32]byte
		copy(other[:], unhex0x(t, v.Tree.Leaves[(i+1)%len(v.Tree.Leaves)]))
		require.False(t, m.Proves(other), "another leaf under leaf %d's branch", i)
		wrongIndex := *m
		wrongIndex.Index ^= 1
		require.False(t, wrongIndex.Proves(leaf), "leaf %d at the wrong index", i)
	}

	_, err := types.DecodeDeliveryMetadata(make([]byte, types.DeliveryMetadataBytes-1))
	require.Error(t, err)
	_, err = types.DecodeDeliveryMetadata(make([]byte, types.DeliveryMetadataBytes+1))
	require.Error(t, err)
}

func TestIdentityDigestMatchesRust(t *testing.T) {
	v := loadVectors(t)

	id := &types.EnclaveIdentity{
		MrTd:        unhex(t, v.Identity.MrTd),
		Rtmr0:       unhex(t, v.Identity.Rtmr0),
		Rtmr1:       unhex(t, v.Identity.Rtmr1),
		Rtmr2:       unhex(t, v.Identity.Rtmr2),
		OsImageHash: unhex(t, v.Identity.OsImageHash),
		ComposeHash: unhex(t, v.Identity.ComposeHash),
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
		"rtmr0":         func(i *types.EnclaveIdentity) { i.Rtmr0[0] ^= 1 },
		"rtmr1":         func(i *types.EnclaveIdentity) { i.Rtmr1[0] ^= 1 },
		"rtmr2":         func(i *types.EnclaveIdentity) { i.Rtmr2[0] ^= 1 },
		"os_image_hash": func(i *types.EnclaveIdentity) { i.OsImageHash[0] ^= 1 },
		"compose_hash":  func(i *types.EnclaveIdentity) { i.ComposeHash[0] ^= 1 },
		"key_provider":  func(i *types.EnclaveIdentity) { i.KeyProvider[0] ^= 1 },
	} {
		t.Run("digest depends on "+name, func(t *testing.T) {
			other := &types.EnclaveIdentity{
				MrTd:        append([]byte{}, id.MrTd...),
				Rtmr0:       append([]byte{}, id.Rtmr0...),
				Rtmr1:       append([]byte{}, id.Rtmr1...),
				Rtmr2:       append([]byte{}, id.Rtmr2...),
				OsImageHash: append([]byte{}, id.OsImageHash...),
				ComposeHash: append([]byte{}, id.ComposeHash...),
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
