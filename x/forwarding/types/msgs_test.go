package types_test

import (
	"strings"
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v8/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("celestia", "celestiapub")
}

func TestNewMsgForward(t *testing.T) {
	signer := "celestia1qperwt9wrnkg5k9e5gzfgjppzpqhyav5j24d66"
	forwardAddr := "celestia1fl48vsnmsdzcv85q5d2q4z5ajdha8yu3h6cprl"
	destDomain := uint32(42161)
	destRecipient := "0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	maxIgpFee := sdk.NewCoin("utia", math.NewInt(1000))

	msg := types.NewMsgForward(signer, forwardAddr, destDomain, destRecipient, maxIgpFee)

	assert.Equal(t, signer, msg.Signer)
	assert.Equal(t, forwardAddr, msg.ForwardAddr)
	assert.Equal(t, destDomain, msg.DestDomain)
	assert.Equal(t, destRecipient, msg.DestRecipient)
	assert.Equal(t, maxIgpFee, msg.MaxIgpFee)
}

func TestNewSuccessResult(t *testing.T) {
	denom := "utia"
	amount := math.NewInt(1000000)
	messageId := "0xabcdef1234567890"

	result := types.NewSuccessResult(denom, amount, messageId)

	assert.Equal(t, denom, result.Denom)
	assert.Equal(t, amount, result.Amount)
	assert.Equal(t, messageId, result.MessageId)
	assert.True(t, result.Success)
	assert.Empty(t, result.Error)
}

func TestNewFailureResult(t *testing.T) {
	denom := "utia"
	amount := math.NewInt(500000)
	errMsg := "no warp route to destination domain"

	result := types.NewFailureResult(denom, amount, errMsg)

	assert.Equal(t, denom, result.Denom)
	assert.Equal(t, amount, result.Amount)
	assert.Empty(t, result.MessageId)
	assert.False(t, result.Success)
	assert.Equal(t, errMsg, result.Error)
}

func TestMsgForwardValidateBasic(t *testing.T) {
	validSignerBytes := []byte("testsigner__________")      // 20 bytes
	validForwardAddrBytes := []byte("forwardaddr_________") // 20 bytes

	validSigner := sdk.AccAddress(validSignerBytes).String()
	validForwardAddr := sdk.AccAddress(validForwardAddrBytes).String()
	validDestRecipient := "0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	// util.DecodeHexAddress accepts addresses with or without 0x prefix
	validDestRecipientNoPrefix := "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	validMaxIgpFee := sdk.NewCoin("utia", math.NewInt(1000))

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
			name: "dest recipient too long (33 bytes)",
			msg: &types.MsgForward{
				Signer:        validSigner,
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: "0x" + strings.Repeat("ab", 33), // 33 bytes, should be 32
				MaxIgpFee:     validMaxIgpFee,
			},
			expectError: true,
			errorMsg:    "invalid hex address length",
		},
		{
			name: "whitespace-only signer",
			msg: &types.MsgForward{
				Signer:        "   ",
				ForwardAddr:   validForwardAddr,
				DestDomain:    1,
				DestRecipient: validDestRecipient,
				MaxIgpFee:     validMaxIgpFee,
			},
			expectError: true,
			errorMsg:    "invalid signer",
		},
		{
			name: "whitespace-only forward address",
			msg: &types.MsgForward{
				Signer:        validSigner,
				ForwardAddr:   "   ",
				DestDomain:    1,
				DestRecipient: validDestRecipient,
				MaxIgpFee:     validMaxIgpFee,
			},
			expectError: true,
			errorMsg:    "invalid forward address",
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
