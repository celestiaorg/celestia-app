package types_test

import (
	"strings"
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestMsgExecuteForwarding_ValidateBasic(t *testing.T) {
	// Generate valid addresses using SDK defaults (cosmos1 prefix)
	validSignerBytes := []byte("testsigner__________") // 20 bytes
	validForwardAddrBytes := []byte("forwardaddr_________") // 20 bytes

	validSigner := sdk.AccAddress(validSignerBytes).String()
	validForwardAddr := sdk.AccAddress(validForwardAddrBytes).String()
	validDestRecipient := "0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	// util.DecodeHexAddress accepts addresses with or without 0x prefix
	validDestRecipientNoPrefix := "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

	t.Logf("Using signer: %s", validSigner)
	t.Logf("Using forward addr: %s", validForwardAddr)

	testCases := []struct {
		name        string
		msg         *types.MsgExecuteForwarding
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid message",
			msg: &types.MsgExecuteForwarding{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: validDestRecipient,
			},
			expectError: false,
		},
		{
			name: "valid message with zero domain",
			msg: &types.MsgExecuteForwarding{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    0,
				DestRecipient: validDestRecipient,
			},
			expectError: false,
		},
		{
			name: "valid message with max domain",
			msg: &types.MsgExecuteForwarding{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    ^uint32(0),
				DestRecipient: validDestRecipient,
			},
			expectError: false,
		},
		{
			name: "valid message dest_recipient without 0x prefix",
			msg: &types.MsgExecuteForwarding{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: validDestRecipientNoPrefix,
			},
			expectError: false, // util.DecodeHexAddress accepts without 0x prefix
		},
		{
			name: "empty signer",
			msg: &types.MsgExecuteForwarding{
				Signer:        "",
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: validDestRecipient,
			},
			expectError: true,
			errorMsg:    "invalid signer",
		},
		{
			name: "invalid signer address",
			msg: &types.MsgExecuteForwarding{
				Signer:        "invalid-address",
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: validDestRecipient,
			},
			expectError: true,
			errorMsg:    "invalid signer",
		},
		{
			name: "empty forward address",
			msg: &types.MsgExecuteForwarding{
				Signer:        validSigner,
				ForwardAddr:   "",
				DestDomain:    1,
				DestRecipient: validDestRecipient,
			},
			expectError: true,
			errorMsg:    "invalid forward address",
		},
		{
			name: "invalid forward address",
			msg: &types.MsgExecuteForwarding{
				Signer:        validSigner,
				ForwardAddr:   "not-a-valid-address",
				DestDomain:    1,
				DestRecipient: validDestRecipient,
			},
			expectError: true,
			errorMsg:    "invalid forward address",
		},
		{
			name: "empty dest recipient",
			msg: &types.MsgExecuteForwarding{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: "",
			},
			expectError: true,
			errorMsg:    "invalid dest_recipient hex format",
		},
		{
			name: "dest recipient too short",
			msg: &types.MsgExecuteForwarding{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: "0xdeadbeef",
			},
			expectError: true,
			errorMsg:    "invalid dest_recipient hex format",
		},
		{
			name: "dest recipient invalid hex",
			msg: &types.MsgExecuteForwarding{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: "0x" + strings.Repeat("zz", 32),
			},
			expectError: true,
			errorMsg:    "invalid dest_recipient hex format",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Note: GetSigners() is inferred from proto annotation (cosmos.msg.v1.signer)
// Route() and Type() methods are deprecated in newer Cosmos SDK versions
// These methods are no longer required for sdk.Msg interface

func TestMsgUpdateForwardingParamsValidateBasic(t *testing.T) {
	validAuthorityBytes := []byte("authority___________") // 20 bytes
	validAuthority := sdk.AccAddress(validAuthorityBytes).String()

	testCases := []struct {
		name        string
		msg         *types.MsgUpdateForwardingParams
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid message with default params",
			msg: &types.MsgUpdateForwardingParams{
				Authority: validAuthority,
				Params:    types.DefaultParams(),
			},
			expectError: false,
		},
		{
			name: "valid message with custom MinForwardAmount",
			msg: &types.MsgUpdateForwardingParams{
				Authority: validAuthority,
				Params: types.Params{
					MinForwardAmount: math.NewInt(1000),
				},
			},
			expectError: false,
		},
		{
			name: "empty authority",
			msg: &types.MsgUpdateForwardingParams{
				Authority: "",
				Params:    types.DefaultParams(),
			},
			expectError: true,
			errorMsg:    "invalid authority",
		},
		{
			name: "invalid authority address",
			msg: &types.MsgUpdateForwardingParams{
				Authority: "invalid-address",
				Params:    types.DefaultParams(),
			},
			expectError: true,
			errorMsg:    "invalid authority",
		},
		{
			name: "negative MinForwardAmount",
			msg: &types.MsgUpdateForwardingParams{
				Authority: validAuthority,
				Params: types.Params{
					MinForwardAmount: math.NewInt(-1),
				},
			},
			expectError: true,
			errorMsg:    "below minimum",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
