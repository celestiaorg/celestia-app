package types_test

import (
	"bytes"
	"crypto/sha512"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
	"github.com/stretchr/testify/require"
)

// dstack 0.6 records the app in RTMR3 as canonical (v2) events and no longer
// emits mr-kms. An identity pinning os-image-hash, compose-hash and key-provider
// must verify against such a log.
func TestDstack06EventLogVerifies(t *testing.T) {
	event := func(name string, payload []byte) types.EventLog {
		pre := types.EventPreimageV2(types.DstackRuntimeEventType, name, payload)
		return types.EventLog{
			IMR:          types.ApplicationIMR,
			EventType:    types.DstackRuntimeEventType,
			Digest:       types.Digest(sha512.Sum384(pre)),
			Event:        name,
			EventPayload: payload,
		}
	}
	osImage := bytes.Repeat([]byte{0x0a}, 32)
	compose := bytes.Repeat([]byte{0x0b}, 32)
	keyProvider := []byte(`{"name":"kms","id":"3059"}`)
	log := []types.EventLog{
		event("app-id", bytes.Repeat([]byte{0x01}, 20)),
		event("compose-hash", compose),
		event("instance-id", bytes.Repeat([]byte{0x02}, 20)),
		event("boot-mr-done", nil),
		event("os-image-hash", osImage),
		event("key-provider", keyProvider),
	}
	measured := &types.Measurements{Rtmrs: types.ReplayEventLogs(log)}
	copy(measured.MrTd[:], bytes.Repeat([]byte{0x0c}, types.MrTdLen))

	id := &types.EnclaveIdentity{
		MrTd:        measured.MrTd[:],
		Rtmr0:       measured.Rtmrs[0][:],
		Rtmr1:       measured.Rtmrs[1][:],
		Rtmr2:       measured.Rtmrs[2][:],
		OsImageHash: osImage,
		ComposeHash: compose,
		KeyProvider: keyProvider,
	}
	require.NoError(t, id.Validate())
	require.NoError(t, types.VerifyEnclaveIdentity(id, measured, log, types.ReplayEventLogs(log)))

	t.Run("a different key provider is refused", func(t *testing.T) {
		other := *id
		other.KeyProvider = []byte(`{"name":"kms","id":"3060"}`)
		err := types.VerifyEnclaveIdentity(&other, measured, log, types.ReplayEventLogs(log))
		require.ErrorIs(t, err, types.ErrIdentityMismatch)
	})
	t.Run("a log without os-image-hash is refused", func(t *testing.T) {
		short := append([]types.EventLog{}, log[:4]...)
		short = append(short, log[5])
		m := &types.Measurements{MrTd: measured.MrTd, Rtmrs: types.ReplayEventLogs(short)}
		err := types.VerifyEnclaveIdentity(id, m, short, types.ReplayEventLogs(short))
		require.ErrorIs(t, err, types.ErrIdentityMismatch)
	})
}
