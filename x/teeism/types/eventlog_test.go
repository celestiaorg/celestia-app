package types_test

import (
	"crypto/sha512"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
	"github.com/stretchr/testify/require"
)

// Every pinned value is read through one pass over the log, and the pass refuses
// a log in which any measured entry does not commit to its own text.
func TestIndexApplicationEvents(t *testing.T) {
	_, _, log := realIdentity(t)
	events, err := types.IndexApplicationEvents(log, types.PinnedEventNames())
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
	_, err = types.IndexApplicationEvents(tampered, types.PinnedEventNames())
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

// A name of nothing but ':' is the worst case for the prefix rule. Checked for
// every prefix it hashed quadratically many bytes before any pinned measurement
// was compared; checked for the pinned names only it is one pass, and it leaves
// the pinned values readable.
func TestIndexApplicationEventsColonFloodIsLinear(t *testing.T) {
	_, _, log := realIdentity(t)
	flood := types.EventLog{
		IMR:       types.ApplicationIMR,
		EventType: types.DstackRuntimeEventType,
		Event:     strings.Repeat(":", types.MaxEventLogBytes),
	}
	flooded := append(append([]types.EventLog(nil), log...), flood)

	start := time.Now()
	for range 100 {
		events, err := types.IndexApplicationEvents(flooded, types.PinnedEventNames())
		require.NoError(t, err)
		for _, name := range types.PinnedEventNames() {
			_, ok := events.Get(name)
			require.True(t, ok, name)
		}
	}
	// The quadratic loop took about 60 ms per call, so 6 s for these 100; one
	// pass takes about a microsecond. The bound leaves room for a slow or
	// instrumented runner.
	require.Less(t, time.Since(start), time.Second)
}

// The prefix rule still refuses a pinned name that another IMR 3 entry extends
// up to a ':'.
func TestIndexApplicationEventsPoisonsPinnedPrefix(t *testing.T) {
	_, _, log := realIdentity(t)
	extended := append(append([]types.EventLog(nil), log...), types.EventLog{
		IMR:       types.ApplicationIMR,
		EventType: types.DstackRuntimeEventType,
		Event:     types.EventComposeHash + ":x",
	})
	events, err := types.IndexApplicationEvents(extended, types.PinnedEventNames())
	require.NoError(t, err)
	_, ok := events.Get(types.EventComposeHash)
	require.False(t, ok)
	_, ok = events.Get(types.EventOsImageHash)
	require.True(t, ok)
}

func BenchmarkIndexApplicationEventsColonFlood(b *testing.B) {
	log := []types.EventLog{{
		IMR:       types.ApplicationIMR,
		EventType: types.DstackRuntimeEventType,
		Event:     strings.Repeat(":", types.MaxEventLogBytes),
	}}
	names := types.PinnedEventNames()
	for b.Loop() {
		_, _ = types.IndexApplicationEvents(log, names)
	}
}
