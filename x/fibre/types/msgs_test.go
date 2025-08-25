package types_test

import (
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestMsgSetFibreProviderInfo_ValidateBasic(t *testing.T) {
	validValidatorAddr := sdk.ValAddress("cosmosvaloper1xyerxdp4xcmnswfsxyerxdp4xcmnswfs0t7p50")
	
	tests := []struct {
		name        string
		msg         *types.MsgSetFibreProviderInfo
		expectedErr string
	}{
		{
			name: "valid message",
			msg: &types.MsgSetFibreProviderInfo{
				ValidatorAddress: validValidatorAddr.String(),
				IpAddress:        "192.168.1.1",
			},
			expectedErr: "",
		},
		{
			name: "valid message with IPv6",
			msg: &types.MsgSetFibreProviderInfo{
				ValidatorAddress: validValidatorAddr.String(),
				IpAddress:        "2001:db8::1",
			},
			expectedErr: "",
		},
		{
			name: "valid message with maximum length IP",
			msg: &types.MsgSetFibreProviderInfo{
				ValidatorAddress: validValidatorAddr.String(),
				IpAddress:        strings.Repeat("a", types.MaxIPAddressLength),
			},
			expectedErr: "",
		},
		{
			name: "invalid validator address",
			msg: &types.MsgSetFibreProviderInfo{
				ValidatorAddress: "invalid-address",
				IpAddress:        "192.168.1.1",
			},
			expectedErr: "invalid validator address",
		},
		{
			name: "empty validator address",
			msg: &types.MsgSetFibreProviderInfo{
				ValidatorAddress: "",
				IpAddress:        "192.168.1.1",
			},
			expectedErr: "invalid validator address",
		},
		{
			name: "empty IP address",
			msg: &types.MsgSetFibreProviderInfo{
				ValidatorAddress: validValidatorAddr.String(),
				IpAddress:        "",
			},
			expectedErr: "IP address cannot be empty",
		},
		{
			name: "whitespace only IP address",
			msg: &types.MsgSetFibreProviderInfo{
				ValidatorAddress: validValidatorAddr.String(),
				IpAddress:        "   ",
			},
			expectedErr: "IP address cannot be empty",
		},
		{
			name: "IP address too long",
			msg: &types.MsgSetFibreProviderInfo{
				ValidatorAddress: validValidatorAddr.String(),
				IpAddress:        strings.Repeat("a", types.MaxIPAddressLength+1),
			},
			expectedErr: "IP address too long",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.expectedErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedErr)
			}
		})
	}
}

func TestMsgSetFibreProviderInfo_GetSigners(t *testing.T) {
	validatorAddr := sdk.ValAddress("cosmosvaloper1xyerxdp4xcmnswfsxyerxdp4xcmnswfs0t7p50")
	msg := &types.MsgSetFibreProviderInfo{
		ValidatorAddress: validatorAddr.String(),
		IpAddress:        "192.168.1.1",
	}
	
	signers := msg.GetSigners()
	require.Len(t, signers, 1)
	
	expectedSigner := sdk.AccAddress(validatorAddr)
	require.Equal(t, expectedSigner, signers[0])
}

func TestMsgSetFibreProviderInfo_GetSigners_InvalidAddress(t *testing.T) {
	msg := &types.MsgSetFibreProviderInfo{
		ValidatorAddress: "invalid-address",
		IpAddress:        "192.168.1.1",
	}
	
	require.Panics(t, func() {
		msg.GetSigners()
	})
}

func TestMsgRemoveFibreProviderInfo_ValidateBasic(t *testing.T) {
	validValidatorAddr := sdk.ValAddress("cosmosvaloper1xyerxdp4xcmnswfsxyerxdp4xcmnswfs0t7p50")
	validRemoverAddr := sdk.AccAddress("cosmos1xyerxdp4xcmnswfsxyerxdp4xcmnswfs8h4sjp")
	
	tests := []struct {
		name        string
		msg         *types.MsgRemoveFibreProviderInfo
		expectedErr string
	}{
		{
			name: "valid message",
			msg: &types.MsgRemoveFibreProviderInfo{
				ValidatorAddress: validValidatorAddr.String(),
				RemoverAddress:   validRemoverAddr.String(),
			},
			expectedErr: "",
		},
		{
			name: "invalid validator address",
			msg: &types.MsgRemoveFibreProviderInfo{
				ValidatorAddress: "invalid-validator",
				RemoverAddress:   validRemoverAddr.String(),
			},
			expectedErr: "invalid validator address",
		},
		{
			name: "empty validator address",
			msg: &types.MsgRemoveFibreProviderInfo{
				ValidatorAddress: "",
				RemoverAddress:   validRemoverAddr.String(),
			},
			expectedErr: "invalid validator address",
		},
		{
			name: "invalid remover address",
			msg: &types.MsgRemoveFibreProviderInfo{
				ValidatorAddress: validValidatorAddr.String(),
				RemoverAddress:   "invalid-remover",
			},
			expectedErr: "invalid remover address",
		},
		{
			name: "empty remover address",
			msg: &types.MsgRemoveFibreProviderInfo{
				ValidatorAddress: validValidatorAddr.String(),
				RemoverAddress:   "",
			},
			expectedErr: "invalid remover address",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.expectedErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedErr)
			}
		})
	}
}

func TestMsgRemoveFibreProviderInfo_GetSigners(t *testing.T) {
	validatorAddr := sdk.ValAddress("cosmosvaloper1xyerxdp4xcmnswfsxyerxdp4xcmnswfs0t7p50")
	removerAddr := sdk.AccAddress("cosmos1xyerxdp4xcmnswfsxyerxdp4xcmnswfs8h4sjp")
	
	msg := &types.MsgRemoveFibreProviderInfo{
		ValidatorAddress: validatorAddr.String(),
		RemoverAddress:   removerAddr.String(),
	}
	
	signers := msg.GetSigners()
	require.Len(t, signers, 1)
	require.Equal(t, removerAddr, signers[0])
}

func TestMsgRemoveFibreProviderInfo_GetSigners_InvalidAddress(t *testing.T) {
	validatorAddr := sdk.ValAddress("cosmosvaloper1xyerxdp4xcmnswfsxyerxdp4xcmnswfs0t7p50")
	
	msg := &types.MsgRemoveFibreProviderInfo{
		ValidatorAddress: validatorAddr.String(),
		RemoverAddress:   "invalid-address",
	}
	
	require.Panics(t, func() {
		msg.GetSigners()
	})
}

func TestMaxIPAddressLength(t *testing.T) {
	// Verify the constant is set to the expected value
	require.Equal(t, 45, types.MaxIPAddressLength)
}

// Test message interface compliance
func TestMessageInterfaceCompliance(t *testing.T) {
	validatorAddr := sdk.ValAddress("cosmosvaloper1xyerxdp4xcmnswfsxyerxdp4xcmnswfs0t7p50")
	removerAddr := sdk.AccAddress("cosmos1xyerxdp4xcmnswfsxyerxdp4xcmnswfs8h4sjp")
	
	// Test MsgSetFibreProviderInfo implements sdk.Msg
	var _ sdk.Msg = (*types.MsgSetFibreProviderInfo)(nil)
	
	msg1 := &types.MsgSetFibreProviderInfo{
		ValidatorAddress: validatorAddr.String(),
		IpAddress:        "192.168.1.1",
	}
	
	// Test that the message can be used as sdk.Msg
	var sdkMsg sdk.Msg = msg1
	require.NotNil(t, sdkMsg)
	
	// Test MsgRemoveFibreProviderInfo implements sdk.Msg
	var _ sdk.Msg = (*types.MsgRemoveFibreProviderInfo)(nil)
	
	msg2 := &types.MsgRemoveFibreProviderInfo{
		ValidatorAddress: validatorAddr.String(),
		RemoverAddress:   removerAddr.String(),
	}
	
	// Test that the message can be used as sdk.Msg
	sdkMsg = msg2
	require.NotNil(t, sdkMsg)
}