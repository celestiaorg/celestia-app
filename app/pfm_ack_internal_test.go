package app

import (
	"testing"

	"cosmossdk.io/log"
	sdk "github.com/cosmos/cosmos-sdk/types"
	capabilitytypes "github.com/cosmos/ibc-go/modules/capability/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v8/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
)

type recordingICS4Wrapper struct {
	porttypes.ICS4Wrapper
	written ibcexported.Acknowledgement
}

func (r *recordingICS4Wrapper) WriteAcknowledgement(_ sdk.Context, _ *capabilitytypes.Capability, _ ibcexported.PacketI, ack ibcexported.Acknowledgement) error {
	r.written = ack
	return nil
}

// TestPFMICS4WrapperFixesErrorAcks covers the acknowledgements that
// packet-forward-middleware writes for forwarded packets, for example after
// the forwarded packet times out with no retries left.
func TestPFMICS4WrapperFixesErrorAcks(t *testing.T) {
	fixed := `{"error":"ABCI code: 11151: error handling packet: see events for details"}`
	testCases := []struct {
		name      string
		ack       ibcexported.Acknowledgement
		want      string
		wantEvent bool
	}{
		{
			name:      "timeout error ack",
			ack:       channeltypes.Acknowledgement{Response: &channeltypes.Acknowledgement_Error{Error: "packet-forward-middleware error: giving up on packet on channel (channel-1) port (transfer) after max retries"}},
			want:      fixed,
			wantEvent: true,
		},
		{
			name:      "relayed downstream error ack with source frame",
			ack:       channeltypes.Acknowledgement{Response: &channeltypes.Acknowledgement_Error{Error: "packet-forward-middleware error: invalid identifier [x/validate.go:41]"}},
			want:      fixed,
			wantEvent: true,
		},
		{
			name: "ibc-go error ack is unchanged",
			ack:  channeltypes.NewErrorAcknowledgement(channeltypes.ErrInvalidPacket),
			want: string(channeltypes.NewErrorAcknowledgement(channeltypes.ErrInvalidPacket).Acknowledgement()),
		},
		{
			name: "result ack is unchanged",
			ack:  channeltypes.NewResultAcknowledgement([]byte("packet-forward-middleware error: x")),
			want: string(channeltypes.NewResultAcknowledgement([]byte("packet-forward-middleware error: x")).Acknowledgement()),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())
			inner := &recordingICS4Wrapper{}
			require.NoError(t, pfmICS4Wrapper{inner}.WriteAcknowledgement(ctx, nil, channeltypes.Packet{}, tc.ack))
			require.Equal(t, tc.want, string(inner.written.Acknowledgement()))
			events := ctx.EventManager().Events()
			if tc.wantEvent {
				require.Len(t, events, 1)
				require.Equal(t, EventTypePacketForwardError, events[0].Type)
			} else {
				require.Empty(t, events)
			}
		})
	}
}
