package app_test

import (
	"encoding/json"
	"testing"

	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

// TestPFMErrorAckIsFixed verifies that packet-forward-middleware error
// acknowledgements committed on this chain do not contain Go error text.
// Different invalid forward memos must produce the same acknowledgement.
func TestPFMErrorAckIsFixed(t *testing.T) {
	testApp := testutil.NewTestApp()
	ctx := testApp.NewUncachedContext(false, cmtproto.Header{})
	transferStack, ok := testApp.IBCKeeper.Router.GetRoute(ibctransfertypes.ModuleName)
	require.True(t, ok)

	memos := map[string]string{
		"blank port":         `{"forward":{"receiver":"cosmos1abc","port":"","channel":"channel-0"}}`,
		"separator in chan":  `{"forward":{"receiver":"cosmos1abc","port":"transfer","channel":"channel/0"}}`,
		"empty receiver":     `{"forward":{"receiver":"","port":"transfer","channel":"channel-0"}}`,
		"invalid forward":    `{"forward":{"receiver":1}}`,
		"negative retries":   `{"forward":{"receiver":"cosmos1abc","port":"transfer","channel":"channel-0","retries":-1}}`,
	}

	// The pinned bytes are part of consensus: ibc-go commits their hash.
	want := `{"error":"ABCI code: 11151: error handling packet: see events for details"}`
	for name, memo := range memos {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(ibctransfertypes.FungibleTokenPacketData{
				Denom: "uatom", Amount: "1", Sender: "remote", Receiver: "pfm", Memo: memo,
			})
			require.NoError(t, err)
			packet := channeltypes.Packet{
				Sequence:           1,
				SourcePort:         "transfer",
				SourceChannel:      "channel-0",
				DestinationPort:    "transfer",
				DestinationChannel: "channel-1",
				Data:               data,
			}
			ack := transferStack.OnRecvPacket(ctx, packet, nil)
			require.NotNil(t, ack)
			require.False(t, ack.Success())
			require.Equal(t, want, string(ack.Acknowledgement()))
		})
	}
}
