package types_test

import (
	"bytes"
	"crypto/sha512"
	"encoding/json"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
	"github.com/stretchr/testify/require"
)

// relabel renames an entry while keeping the digest it contributes to the replay.
func relabel(e *types.EventLog, name string) {
	e.Digest = types.Digest(sha512.Sum384(types.EventPreimageV1(e.EventType, e.Event, e.EventPayload)))
	e.Event = name
}

func selfCommitting(imr uint32, name string, payload []byte) types.EventLog {
	return types.EventLog{
		IMR:          imr,
		EventType:    types.DstackRuntimeEventType,
		Digest:       types.Digest(sha512.Sum384(types.EventPreimageV1(types.DstackRuntimeEventType, name, payload))),
		Event:        name,
		EventPayload: payload,
	}
}

// A genuine quote from one app must not pass as a different, pinned app.
func TestForgedEventLogIsRefused(t *testing.T) {
	id, measured, log := realIdentity(t)
	victim := *id
	victim.ComposeHash = bytes.Repeat([]byte{0xbb}, 32)

	t.Run("a pinned value outside IMR 3 is refused", func(t *testing.T) {
		// A CVM whose IMR 3 never recorded a compose-hash, offering one from IMR
		// 7, which no pinned register covers.
		var forged []types.EventLog
		for _, e := range log {
			if e.Event != types.EventComposeHash {
				forged = append(forged, e)
			}
		}
		forged = append(forged, selfCommitting(7, types.EventComposeHash, victim.ComposeHash))
		m := *measured
		m.Rtmrs = types.ReplayEventLogs(forged)
		err := types.VerifyEnclaveIdentity(&victim, &m, forged, m.Rtmrs)
		require.ErrorIs(t, err, types.ErrIdentityMismatch)
	})

	t.Run("a relabelled IMR 3 entry is refused", func(t *testing.T) {
		// As if the app itself had extended RTMR3 with a compose-hash event, and
		// the measured one were renamed out of its way.
		forged := append([]types.EventLog{}, log...)
		for i := range forged {
			if forged[i].Event == types.EventComposeHash {
				relabel(&forged[i], "relabelled")
			}
		}
		forged = append(forged, selfCommitting(types.ApplicationIMR, types.EventComposeHash, victim.ComposeHash))
		m := *measured
		m.Rtmrs = types.ReplayEventLogs(forged)
		err := types.VerifyEnclaveIdentity(&victim, &m, forged, m.Rtmrs)
		require.ErrorIs(t, err, types.ErrIdentityMismatch)
	})

	t.Run("a re-split IMR 3 entry is refused", func(t *testing.T) {
		// The v1 preimage type || ":" || name || ":" || payload does not mark
		// where the name ends, so moving the boundary to a ':' inside the
		// payload renames an entry without changing what it extends RTMR3
		// with. key-provider's JSON payload always has one.
		evil := []byte(`{"name":"kms","id":"evil"}`)
		victim := *id
		victim.KeyProvider = evil
		genuine := append(append([]types.EventLog{}, log...),
			selfCommitting(types.ApplicationIMR, types.EventKeyProvider, evil))
		m := *measured
		m.Rtmrs = types.ReplayEventLogs(genuine)

		forged := append([]types.EventLog{}, genuine...)
		for i := range forged {
			e := &forged[i]
			if e.Event == types.EventKeyProvider && e.Digest.IsZero() {
				k := bytes.IndexByte(e.EventPayload, ':')
				require.Positive(t, k)
				e.Event += ":" + string(e.EventPayload[:k])
				e.EventPayload = e.EventPayload[k+1:]
			}
		}
		require.Equal(t, m.Rtmrs, types.ReplayEventLogs(forged), "the re-split must not change the replay")
		err := types.VerifyEnclaveIdentity(&victim, &m, forged, m.Rtmrs)
		require.ErrorIs(t, err, types.ErrIdentityMismatch)
	})
}

// A different kernel, initrd or VM configuration changes RTMR0-2 and must be
// refused even when the event log matches.
func TestUnpinnedBootChainIsRefused(t *testing.T) {
	id, measured, log := realIdentity(t)
	for i := range 3 {
		m := *measured
		m.Rtmrs[i][0] ^= 0xff
		err := types.VerifyEnclaveIdentity(id, &m, log, m.Rtmrs)
		require.ErrorIs(t, err, types.ErrIdentityMismatch, "rtmr%d", i)
	}
}

func TestEnclaveIdentityRequiresRtmrs(t *testing.T) {
	id, _, _ := realIdentity(t)
	for _, field := range []*[]byte{&id.Rtmr0, &id.Rtmr1, &id.Rtmr2} {
		saved := *field
		*field = saved[:types.DigestLen-1]
		require.ErrorIs(t, id.Validate(), types.ErrInvalidEnclaveIdentity)
		*field = saved
	}
}

// go-tdx-guest checks Intel's signature over the exact "tcbInfo" and
// "enclaveIdentity" keys but reads its verdict case-insensitively, so a second
// key that folds to them would be read unsigned.
func TestCollateralWithShadowedSignedKeyIsRefused(t *testing.T) {
	quote, _, genuine, at := loadRealQuote(t)

	// with appends a top-level member to a JSON object.
	with := func(body []byte, member string) []byte {
		body = bytes.TrimSpace(body)
		out := append([]byte{}, body[:len(body)-1]...)
		return append(append(out, ","+member...), '}')
	}
	signed := func(body []byte, key string) string {
		var m map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &m))
		return string(m[key])
	}
	tcb := signed(genuine.TcbInfo, "tcbInfo")
	qe := signed(genuine.QeIdentity, "enclaveIdentity")

	for _, tc := range []struct {
		name string
		edit func(c *types.Collateral)
	}{
		{"case-folded tcbInfo", func(c *types.Collateral) { c.TcbInfo = with(c.TcbInfo, `"TCBINFO":`+tcb) }},
		{"duplicated tcbInfo", func(c *types.Collateral) { c.TcbInfo = with(c.TcbInfo, `"tcbInfo":`+tcb) }},
		{"case-folded enclaveIdentity", func(c *types.Collateral) {
			c.QeIdentity = with(c.QeIdentity, `"ENCLAVEIDENTITY":`+qe)
		}},
		{"misspelled tcbInfo only", func(c *types.Collateral) {
			c.TcbInfo = bytes.Replace(c.TcbInfo, []byte(`"tcbInfo"`), []byte(`"TcbInfo"`), 1)
		}},
		{"trailing data", func(c *types.Collateral) { c.TcbInfo = append(append([]byte{}, c.TcbInfo...), []byte(`{}`)...) }},
		{"not an object", func(c *types.Collateral) { c.QeIdentity = []byte(`[]`) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := *genuine
			tc.edit(&c)
			require.ErrorIs(t, c.Validate(), types.ErrInvalidCollateral)
			_, _, err := types.VerifyQuote(quote, &c, at)
			require.ErrorIs(t, err, types.ErrInvalidCollateral)
		})
	}

	t.Run("an unrelated new field still verifies", func(t *testing.T) {
		c := *genuine
		c.TcbInfo = with(c.TcbInfo, `"futureField":1`)
		_, _, err := types.VerifyQuote(quote, &c, at)
		require.NoError(t, err)
	})
}
