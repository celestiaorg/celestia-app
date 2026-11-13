package types_test

import (
	"crypto/sha512"
	"encoding/json"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
	"github.com/stretchr/testify/require"
)

// Every pinned value is read through one pass over the log, and the pass refuses
// a log in which any measured entry does not commit to its own text.
func TestIndexApplicationEvents(t *testing.T) {
	_, _, log := realIdentity(t)
	events, err := types.IndexApplicationEvents(log)
	require.NoError(t, err)
	for _, name := range []string{types.EventOsImageHash, types.EventComposeHash, types.EventKeyProvider} {
		indexed, ok := events.Get(name)
		require.True(t, ok, name)
		direct, ok := types.GetEventValue(log, name)
		require.True(t, ok, name)
		require.Equal(t, direct, indexed, name)
	}
	_, ok := events.Get("not-an-event")
	require.False(t, ok)

	// dstack writes IMR 3 entries without a digest; give one its genuine digest,
	// then relabel it, as an attacker keeping the replay intact would.
	tampered := append([]types.EventLog(nil), log...)
	for i := range tampered {
		e := &tampered[i]
		if e.IMR == types.ApplicationIMR && e.Event == types.EventComposeHash {
			e.Digest = types.Digest(sha512.Sum384(types.EventPreimageV1(e.EventType, e.Event, e.EventPayload)))
			e.Event += "x"
			break
		}
	}
	_, err = types.IndexApplicationEvents(tampered)
	require.Error(t, err)
	_, ok = types.GetEventValue(tampered, types.EventComposeHash)
	require.False(t, ok)
}

// The entry cap bounds the hashing a log can demand of every validator.
func TestEventLogEntryCap(t *testing.T) {
	entry := map[string]any{"imr": 3, "event_type": types.DstackRuntimeEventType, "digest": "", "event": "e", "event_payload": "00"}
	atCap := make([]any, types.MaxEventLogEntries)
	for i := range atCap {
		atCap[i] = entry
	}
	raw, err := json.Marshal(atCap)
	require.NoError(t, err)
	_, err = types.ParseEventLog(raw)
	require.NoError(t, err)

	raw, err = json.Marshal(append(atCap, entry))
	require.NoError(t, err)
	_, err = types.ParseEventLog(raw)
	require.ErrorIs(t, err, types.ErrMalformedEventLog)
}
