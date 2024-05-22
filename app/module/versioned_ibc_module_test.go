package module_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app/module"
	mocks "github.com/celestiaorg/celestia-app/v2/app/module/mocks"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/ibc-go/v6/modules/core/04-channel/types"
	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
)

func TestVersionedIBCModule(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockWrappedModule := mocks.NewMockIBCModule(ctrl)
	mockNextModule := mocks.NewMockIBCModule(ctrl)

	versionedModule := module.NewVersionedIBCModule(mockWrappedModule, mockNextModule, 2, 2)

	testCases := []struct {
		name          string
		version       uint64
		setupMocks    func(ctx sdk.Context)
		method        func(ctx sdk.Context) (interface{}, error)
		expectedValue interface{}
	}{
		{
			name:    "OnChanOpenInit with supported version",
			version: 2,
			setupMocks: func(ctx sdk.Context) {
				mockWrappedModule.EXPECT().OnChanOpenInit(ctx, types.ORDERED, []string{"connection"}, "port", "channel", nil, types.Counterparty{}, "1").Return("wrapped_version", nil)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return versionedModule.OnChanOpenInit(ctx, types.ORDERED, []string{"connection"}, "port", "channel", nil, types.Counterparty{}, "1")
			},
			expectedValue: "wrapped_version",
		},
		{
			name:    "OnChanOpenInit with unsupported version",
			version: 1,
			setupMocks: func(ctx sdk.Context) {
				mockNextModule.EXPECT().OnChanOpenInit(ctx, types.ORDERED, []string{"connection"}, "port", "channel", nil, types.Counterparty{}, "1").Return("next_version", nil)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return versionedModule.OnChanOpenInit(ctx, types.ORDERED, []string{"connection"}, "port", "channel", nil, types.Counterparty{}, "1")
			},
			expectedValue: "next_version",
		},
		{
			name:    "OnChanOpenTry with supported version",
			version: 2,
			setupMocks: func(ctx sdk.Context) {
				mockWrappedModule.EXPECT().OnChanOpenTry(ctx, types.ORDERED, []string{"connection"}, "port", "channel", nil, types.Counterparty{}, "1").Return("wrapped_version", nil)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return versionedModule.OnChanOpenTry(ctx, types.ORDERED, []string{"connection"}, "port", "channel", nil, types.Counterparty{}, "1")
			},
			expectedValue: "wrapped_version",
		},
		{
			name:    "OnChanOpenTry with unsupported version",
			version: 1,
			setupMocks: func(ctx sdk.Context) {
				mockNextModule.EXPECT().OnChanOpenTry(ctx, types.ORDERED, []string{"connection"}, "port", "channel", nil, types.Counterparty{}, "1").Return("next_version", nil)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return versionedModule.OnChanOpenTry(ctx, types.ORDERED, []string{"connection"}, "port", "channel", nil, types.Counterparty{}, "1")
			},
			expectedValue: "next_version",
		},
		{
			name:    "OnChanOpenAck with supported version",
			version: 2,
			setupMocks: func(ctx sdk.Context) {
				mockWrappedModule.EXPECT().OnChanOpenAck(ctx, "port", "channel", "counterpartyChannelID", "counterpartyVersion").Return(nil)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return nil, versionedModule.OnChanOpenAck(ctx, "port", "channel", "counterpartyChannelID", "counterpartyVersion")
			},
			expectedValue: nil,
		},
		{
			name:    "OnChanOpenAck with unsupported version",
			version: 1,
			setupMocks: func(ctx sdk.Context) {
				mockNextModule.EXPECT().OnChanOpenAck(ctx, "port", "channel", "counterpartyChannelID", "counterpartyVersion").Return(nil)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return nil, versionedModule.OnChanOpenAck(ctx, "port", "channel", "counterpartyChannelID", "counterpartyVersion")
			},
			expectedValue: nil,
		},
		{
			name:    "OnChanOpenConfirm with supported version",
			version: 2,
			setupMocks: func(ctx sdk.Context) {
				mockWrappedModule.EXPECT().OnChanOpenConfirm(ctx, "port", "channel").Return(nil)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return nil, versionedModule.OnChanOpenConfirm(ctx, "port", "channel")
			},
			expectedValue: nil,
		},
		{
			name:    "OnChanOpenConfirm with unsupported version",
			version: 1,
			setupMocks: func(ctx sdk.Context) {
				mockNextModule.EXPECT().OnChanOpenConfirm(ctx, "port", "channel").Return(nil)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return nil, versionedModule.OnChanOpenConfirm(ctx, "port", "channel")
			},
			expectedValue: nil,
		},
		{
			name:    "OnChanCloseInit with supported version",
			version: 2,
			setupMocks: func(ctx sdk.Context) {
				mockWrappedModule.EXPECT().OnChanCloseInit(ctx, "port", "channel").Return(nil)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return nil, versionedModule.OnChanCloseInit(ctx, "port", "channel")
			},
			expectedValue: nil,
		},
		{
			name:    "OnChanCloseInit with unsupported version",
			version: 1,
			setupMocks: func(ctx sdk.Context) {
				mockNextModule.EXPECT().OnChanCloseInit(ctx, "port", "channel").Return(nil)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return nil, versionedModule.OnChanCloseInit(ctx, "port", "channel")
			},
			expectedValue: nil,
		},
		{
			name:    "OnChanCloseConfirm with supported version",
			version: 2,
			setupMocks: func(ctx sdk.Context) {
				mockWrappedModule.EXPECT().OnChanCloseConfirm(ctx, "port", "channel").Return(nil)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return nil, versionedModule.OnChanCloseConfirm(ctx, "port", "channel")
			},
			expectedValue: nil,
		},
		{
			name:    "OnChanCloseConfirm with unsupported version",
			version: 1,
			setupMocks: func(ctx sdk.Context) {
				mockNextModule.EXPECT().OnChanCloseConfirm(ctx, "port", "channel").Return(nil)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return nil, versionedModule.OnChanCloseConfirm(ctx, "port", "channel")
			},
			expectedValue: nil,
		},
		{
			name:    "OnRecvPacket with supported version",
			version: 2,
			setupMocks: func(ctx sdk.Context) {
				expectedAck := types.NewResultAcknowledgement([]byte("wrapped_ack"))
				mockWrappedModule.EXPECT().OnRecvPacket(ctx, types.Packet{}, sdk.AccAddress{}).Return(expectedAck)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return versionedModule.OnRecvPacket(ctx, types.Packet{}, sdk.AccAddress{}), nil
			},
			expectedValue: types.NewResultAcknowledgement([]byte("wrapped_ack")),
		},
		{
			name:    "OnRecvPacket with unsupported version",
			version: 1,
			setupMocks: func(ctx sdk.Context) {
				expectedAck := types.NewResultAcknowledgement([]byte("next_ack"))
				mockNextModule.EXPECT().OnRecvPacket(ctx, types.Packet{}, sdk.AccAddress{}).Return(expectedAck)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return versionedModule.OnRecvPacket(ctx, types.Packet{}, sdk.AccAddress{}), nil
			},
			expectedValue: types.NewResultAcknowledgement([]byte("next_ack")),
		},
		{
			name:    "OnAcknowledgementPacket with supported version",
			version: 2,
			setupMocks: func(ctx sdk.Context) {
				mockWrappedModule.EXPECT().OnAcknowledgementPacket(ctx, types.Packet{}, []byte{}, sdk.AccAddress{}).Return(nil)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return nil, versionedModule.OnAcknowledgementPacket(ctx, types.Packet{}, []byte{}, sdk.AccAddress{})
			},
			expectedValue: nil,
		},
		{
			name:    "OnAcknowledgementPacket with unsupported version",
			version: 1,
			setupMocks: func(ctx sdk.Context) {
				mockNextModule.EXPECT().OnAcknowledgementPacket(ctx, types.Packet{}, []byte{}, sdk.AccAddress{}).Return(nil)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return nil, versionedModule.OnAcknowledgementPacket(ctx, types.Packet{}, []byte{}, sdk.AccAddress{})
			},
			expectedValue: nil,
		},
		{
			name:    "OnTimeoutPacket with supported version",
			version: 2,
			setupMocks: func(ctx sdk.Context) {
				mockWrappedModule.EXPECT().OnTimeoutPacket(ctx, types.Packet{}, sdk.AccAddress{}).Return(nil)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return nil, versionedModule.OnTimeoutPacket(ctx, types.Packet{}, sdk.AccAddress{})
			},
			expectedValue: nil,
		},
		{
			name:    "OnTimeoutPacket with unsupported version",
			version: 1,
			setupMocks: func(ctx sdk.Context) {
				mockNextModule.EXPECT().OnTimeoutPacket(ctx, types.Packet{}, sdk.AccAddress{}).Return(nil)
			},
			method: func(ctx sdk.Context) (interface{}, error) {
				return nil, versionedModule.OnTimeoutPacket(ctx, types.Packet{}, sdk.AccAddress{})
			},
			expectedValue: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := sdk.Context{}.WithBlockHeader(tmproto.Header{Version: version.Consensus{App: tc.version}})
			tc.setupMocks(ctx)
			actualValue, err := tc.method(ctx)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedValue, actualValue)
		})
	}
}
