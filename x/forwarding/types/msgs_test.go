package types_test

import (
	"strings"
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func init() {
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("celestia", "celestiapub")
}

func TestMsgForward_ValidateBasic(t *testing.T) {
	validSignerBytes := []byte("testsigner__________")      // 20 bytes
	validForwardAddrBytes := []byte("forwardaddr_________") // 20 bytes

	validSigner := sdk.AccAddress(validSignerBytes).String()
	validForwardAddr := sdk.AccAddress(validForwardAddrBytes).String()
	validDestRecipient := "0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	// util.DecodeHexAddress accepts addresses with or without 0x prefix
	validDestRecipientNoPrefix := "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	validMaxIgpFee := sdk.NewCoin("utia", math.NewInt(1000))

	t.Logf("Using signer: %s", validSigner)
	t.Logf("Using forward addr: %s", validForwardAddr)

	testCases := []struct {
		name        string
		msg         *types.MsgForward
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid message",
			msg: &types.MsgForward{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: validDestRecipient,
				MaxIgpFee:     validMaxIgpFee,
			},
			expectError: false,
		},
		{
			name: "valid message with zero domain",
			msg: &types.MsgForward{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    0,
				DestRecipient: validDestRecipient,
				MaxIgpFee:     validMaxIgpFee,
			},
			expectError: false,
		},
		{
			name: "valid message with max domain",
			msg: &types.MsgForward{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    ^uint32(0),
				DestRecipient: validDestRecipient,
				MaxIgpFee:     validMaxIgpFee,
			},
			expectError: false,
		},
		{
			name: "valid message dest_recipient without 0x prefix",
			msg: &types.MsgForward{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: validDestRecipientNoPrefix,
				MaxIgpFee:     validMaxIgpFee,
			},
			expectError: false, // util.DecodeHexAddress accepts without 0x prefix
		},
		{
			name: "valid message with zero max_igp_fee",
			msg: &types.MsgForward{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: validDestRecipient,
				MaxIgpFee:     sdk.NewCoin("utia", math.ZeroInt()),
			},
			expectError: false,
		},
		{
			name: "empty signer",
			msg: &types.MsgForward{
				Signer:        "",
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: validDestRecipient,
				MaxIgpFee:     validMaxIgpFee,
			},
			expectError: true,
			errorMsg:    "invalid signer",
		},
		{
			name: "invalid signer address",
			msg: &types.MsgForward{
				Signer:        "invalid-address",
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: validDestRecipient,
				MaxIgpFee:     validMaxIgpFee,
			},
			expectError: true,
			errorMsg:    "invalid signer",
		},
		{
			name: "empty forward address",
			msg: &types.MsgForward{
				Signer:        validSigner,
				ForwardAddr:   "",
				DestDomain:    1,
				DestRecipient: validDestRecipient,
				MaxIgpFee:     validMaxIgpFee,
			},
			expectError: true,
			errorMsg:    "invalid forward address",
		},
		{
			name: "invalid forward address",
			msg: &types.MsgForward{
				Signer:        validSigner,
				ForwardAddr:   "not-a-valid-address",
				DestDomain:    1,
				DestRecipient: validDestRecipient,
				MaxIgpFee:     validMaxIgpFee,
			},
			expectError: true,
			errorMsg:    "invalid forward address",
		},
		{
			name: "empty dest recipient",
			msg: &types.MsgForward{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: "",
				MaxIgpFee:     validMaxIgpFee,
			},
			expectError: true,
			errorMsg:    "invalid dest_recipient hex format",
		},
		{
			name: "dest recipient too short",
			msg: &types.MsgForward{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: "0xdeadbeef",
				MaxIgpFee:     validMaxIgpFee,
			},
			expectError: true,
			errorMsg:    "invalid dest_recipient hex format",
		},
		{
			name: "dest recipient invalid hex",
			msg: &types.MsgForward{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: "0x" + strings.Repeat("zz", 32),
				MaxIgpFee:     validMaxIgpFee,
			},
			expectError: true,
			errorMsg:    "invalid dest_recipient hex format",
		},
		{
			name: "invalid max_igp_fee negative amount",
			msg: &types.MsgForward{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: validDestRecipient,
				MaxIgpFee:     sdk.Coin{Denom: "utia", Amount: math.NewInt(-1)},
			},
			expectError: true,
			errorMsg:    "invalid max_igp_fee",
		},
		{
			name: "invalid max_igp_fee empty denom",
			msg: &types.MsgForward{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: validDestRecipient,
				MaxIgpFee:     sdk.Coin{Denom: "", Amount: math.NewInt(1000)},
			},
			expectError: true,
			errorMsg:    "invalid max_igp_fee",
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

func TestMsgUpdateParamsValidateBasic(t *testing.T) {
	validAuthorityBytes := []byte("authority___________") // 20 bytes
	validAuthority := sdk.AccAddress(validAuthorityBytes).String()

	testCases := []struct {
		name        string
		msg         *types.MsgUpdateParams
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid message with default params",
			msg: &types.MsgUpdateParams{
				Authority: validAuthority,
				Params:    types.DefaultParams(),
			},
			expectError: false,
		},
		{
			name: "valid message with custom MinForwardAmount",
			msg: &types.MsgUpdateParams{
				Authority: validAuthority,
				Params: types.Params{
					MinForwardAmount: math.NewInt(1000),
				},
			},
			expectError: false,
		},
		{
			name: "empty authority",
			msg: &types.MsgUpdateParams{
				Authority: "",
				Params:    types.DefaultParams(),
			},
			expectError: true,
			errorMsg:    "invalid authority",
		},
		{
			name: "invalid authority address",
			msg: &types.MsgUpdateParams{
				Authority: "invalid-address",
				Params:    types.DefaultParams(),
			},
			expectError: true,
			errorMsg:    "invalid authority",
		},
		{
			name: "negative MinForwardAmount",
			msg: &types.MsgUpdateParams{
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
