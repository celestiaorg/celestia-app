package types_test

import (
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
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

func TestMsgExecuteForwarding_GetSigners(t *testing.T) {
	signerBytes := []byte("testsigner__________") // 20 bytes
	signer := sdk.AccAddress(signerBytes).String()
	forwardAddrBytes := []byte("forwardaddr_________") // 20 bytes
	forwardAddr := sdk.AccAddress(forwardAddrBytes).String()

	msg := &types.MsgExecuteForwarding{
		Signer:        signer,
		ForwardAddr:   forwardAddr,
		DestDomain:    1,
		DestRecipient: "0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
	}

	signers := msg.GetSigners()
	require.Len(t, signers, 1)

	expectedAddr, err := sdk.AccAddressFromBech32(signer)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, signers[0])
}

// Note: Route() and Type() methods are deprecated in newer Cosmos SDK versions
// These methods are no longer required for sdk.Msg interface
