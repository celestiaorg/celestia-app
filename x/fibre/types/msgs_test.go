package types

import (
	"bytes"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMsgDepositToEscrowValidateBasic(t *testing.T) {
	signer := "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"
	oneCoin := sdk.NewCoin("utia", math.NewInt(1))
	zeroCoin := sdk.NewCoin("utia", math.NewInt(0))
	// negativeCoin does not use sdk.NewCoin because sdk.NewCoin panics if the amount is negative.
	negativeCoin := sdk.Coin{Denom: "utia", Amount: math.NewInt(-100)}

	type testCase struct {
		name    string
		msg     MsgDepositToEscrow
		wantErr error
	}
	testCases := []testCase{
		{
			name: "valid message",
			msg: MsgDepositToEscrow{
				Signer: signer,
				Amount: oneCoin,
			},
			wantErr: nil,
		},
		{
			name: "invalid signer address",
			msg: MsgDepositToEscrow{
				Signer: "invalid-address",
				Amount: oneCoin,
			},
			wantErr: sdkerrors.ErrInvalidAddress,
		},
		{
			name: "empty signer address",
			msg: MsgDepositToEscrow{
				Signer: "",
				Amount: oneCoin,
			},
			wantErr: sdkerrors.ErrInvalidAddress,
		},
		{
			name: "zero amount",
			msg: MsgDepositToEscrow{
				Signer: signer,
				Amount: zeroCoin,
			},
			wantErr: sdkerrors.ErrInvalidCoins,
		},
		{
			name: "negative coin",
			msg: MsgDepositToEscrow{
				Signer: signer,
				Amount: negativeCoin,
			},
			wantErr: sdkerrors.ErrInvalidCoins,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgRequestWithdrawalValidateBasic(t *testing.T) {
	signer := "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"
	oneCoin := sdk.NewCoin("utia", math.NewInt(1))
	zeroCoin := sdk.NewCoin("utia", math.NewInt(0))
	// negativeCoin does not use sdk.NewCoin because sdk.NewCoin panics if the amount is negative.
	negativeCoin := sdk.Coin{Denom: "utia", Amount: math.NewInt(-100)}

	type testCase struct {
		name    string
		msg     MsgRequestWithdrawal
		wantErr error
	}
	testCases := []testCase{
		{
			name: "valid message",
			msg: MsgRequestWithdrawal{
				Signer: signer,
				Amount: oneCoin,
			},
			wantErr: nil,
		},
		{
			name: "invalid signer address",
			msg: MsgRequestWithdrawal{
				Signer: "invalid-address",
				Amount: oneCoin,
			},
			wantErr: sdkerrors.ErrInvalidAddress,
		},
		{
			name: "zero amount",
			msg: MsgRequestWithdrawal{
				Signer: signer,
				Amount: zeroCoin,
			},
			wantErr: sdkerrors.ErrInvalidCoins,
		},
		{
			name: "negative amount",
			msg: MsgRequestWithdrawal{
				Signer: signer,
				Amount: negativeCoin,
			},
			wantErr: sdkerrors.ErrInvalidCoins,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPaymentPromiseValidateBasic(t *testing.T) {
	signerPublicKey := generatePubKeyAny(t)
	namespace := generateNamespace(t)
	blobSize := uint32(1000)
	commitment := generateCommitment(t)
	rowVersion := RowVersionZero
	creationTimestamp := time.Now()
	signature := []byte("valid-signature")
	height := int64(100)
	chainId := "test"

	type testCase struct {
		name    string
		msg     PaymentPromise
		wantErr error
	}
	testCases := []testCase{
		{
			name: "valid payment promise",
			msg: PaymentPromise{
				SignerPublicKey:   signerPublicKey,
				Namespace:         namespace,
				BlobSize:          blobSize,
				Commitment:        commitment,
				RowVersion:        rowVersion,
				CreationTimestamp: creationTimestamp,
				Signature:         signature,
				Height:            height,
				ChainId:           chainId,
			},
			wantErr: nil,
		},
		{
			name: "nil signer public key",
			msg: PaymentPromise{
				SignerPublicKey:   nil,
				Namespace:         namespace,
				BlobSize:          blobSize,
				Commitment:        commitment,
				RowVersion:        rowVersion,
				CreationTimestamp: creationTimestamp,
				Signature:         signature,
				Height:            height,
				ChainId:           chainId,
			},
			wantErr: sdkerrors.ErrInvalidPubKey,
		},
		{
			name: "empty namespace",
			msg: PaymentPromise{
				SignerPublicKey:   signerPublicKey,
				Namespace:         []byte{},
				BlobSize:          blobSize,
				Commitment:        commitment,
				RowVersion:        rowVersion,
				CreationTimestamp: creationTimestamp,
				Signature:         signature,
				Height:            height,
				ChainId:           chainId,
			},
			wantErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "invalid namespace size",
			msg: PaymentPromise{
				SignerPublicKey:   signerPublicKey,
				Namespace:         []byte{1, 2, 3},
				BlobSize:          blobSize,
				Commitment:        commitment,
				RowVersion:        rowVersion,
				CreationTimestamp: creationTimestamp,
				Signature:         signature,
				Height:            height,
				ChainId:           chainId,
			},
			wantErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "zero blob size",
			msg: PaymentPromise{
				SignerPublicKey:   signerPublicKey,
				Namespace:         namespace,
				BlobSize:          0,
				Commitment:        commitment,
				RowVersion:        rowVersion,
				CreationTimestamp: creationTimestamp,
				Signature:         signature,
				Height:            height,
				ChainId:           chainId,
			},
			wantErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "invalid commitment size",
			msg: PaymentPromise{
				SignerPublicKey:   signerPublicKey,
				Namespace:         namespace,
				BlobSize:          blobSize,
				Commitment:        []byte{1, 2, 3}, // wrong size
				RowVersion:        rowVersion,
				CreationTimestamp: creationTimestamp,
				Signature:         signature,
				Height:            height,
				ChainId:           chainId,
			},
			wantErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "unsupported row version",
			msg: PaymentPromise{
				SignerPublicKey:   signerPublicKey,
				Namespace:         namespace,
				BlobSize:          blobSize,
				Commitment:        commitment,
				RowVersion:        999,
				CreationTimestamp: creationTimestamp,
				Signature:         signature,
				Height:            height,
				ChainId:           chainId,
			},
			wantErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "zero height",
			msg: PaymentPromise{
				SignerPublicKey:   signerPublicKey,
				Namespace:         namespace,
				BlobSize:          blobSize,
				Commitment:        commitment,
				RowVersion:        rowVersion,
				CreationTimestamp: creationTimestamp,
				Signature:         signature,
				Height:            0,
				ChainId:           chainId,
			},
			wantErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "empty signature",
			msg: PaymentPromise{
				SignerPublicKey:   signerPublicKey,
				Namespace:         namespace,
				BlobSize:          blobSize,
				Commitment:        commitment,
				RowVersion:        rowVersion,
				CreationTimestamp: creationTimestamp,
				Signature:         []byte{},
				Height:            height,
				ChainId:           chainId,
			},
			wantErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "empty chain ID",
			msg: PaymentPromise{
				SignerPublicKey:   signerPublicKey,
				Namespace:         namespace,
				BlobSize:          blobSize,
				Commitment:        commitment,
				RowVersion:        rowVersion,
				CreationTimestamp: creationTimestamp,
				Signature:         signature,
				Height:            height,
				ChainId:           "",
			},
			wantErr: sdkerrors.ErrInvalidRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgPayForFibre_ValidateBasic(t *testing.T) {
	validAddr := "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"

	// Create a valid PaymentPromise
	privKey := secp256k1.GenPrivKey()
	pubKey := privKey.PubKey()
	pubKeyAny, err := types.NewAnyWithValue(pubKey)
	require.NoError(t, err)

	validNamespace := make([]byte, share.NamespaceSize)
	validNamespace[0] = 0 // version 0
	// Set bytes 19-28 to create a valid user namespace
	for i := 19; i < share.NamespaceSize; i++ {
		validNamespace[i] = 1
	}

	validCommitment := make([]byte, 32)
	for i := range validCommitment {
		validCommitment[i] = byte(i)
	}

	validPromise := PaymentPromise{
		SignerPublicKey:   pubKeyAny,
		Namespace:         validNamespace,
		BlobSize:          1000,
		Commitment:        validCommitment,
		RowVersion:        uint32(share.ShareVersionZero),
		CreationTimestamp: time.Now(),
		Signature:         []byte("valid-signature"),
		Height:            100,
		ChainId:           "celestia-test",
	}

	tests := []struct {
		name    string
		msg     *MsgPayForFibre
		wantErr bool
		errType error
	}{
		{
			name: "valid message",
			msg: &MsgPayForFibre{
				Signer:              validAddr,
				PaymentPromise:      validPromise,
				ValidatorSignatures: [][]byte{[]byte("sig1"), []byte("sig2")},
			},
			wantErr: false,
		},
		{
			name: "invalid signer address",
			msg: &MsgPayForFibre{
				Signer:              "invalid-address",
				PaymentPromise:      validPromise,
				ValidatorSignatures: [][]byte{[]byte("sig1")},
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidAddress,
		},
		{
			name: "no validator signatures",
			msg: &MsgPayForFibre{
				Signer:              validAddr,
				PaymentPromise:      validPromise,
				ValidatorSignatures: [][]byte{},
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "empty validator signature",
			msg: &MsgPayForFibre{
				Signer:              validAddr,
				PaymentPromise:      validPromise,
				ValidatorSignatures: [][]byte{[]byte("sig1"), []byte{}},
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.wantErr {
				require.Error(t, err)
				if tt.errType != nil {
					assert.Contains(t, err.Error(), tt.errType.Error())
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgPaymentPromiseTimeout_ValidateBasic(t *testing.T) {
	validAddr := "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"

	// Create a valid PaymentPromise
	privKey := secp256k1.GenPrivKey()
	pubKey := privKey.PubKey()
	pubKeyAny, err := types.NewAnyWithValue(pubKey)
	require.NoError(t, err)

	validNamespace := make([]byte, share.NamespaceSize)
	validNamespace[0] = 0 // version 0
	// Set bytes 19-28 to create a valid user namespace
	for i := 19; i < share.NamespaceSize; i++ {
		validNamespace[i] = 1
	}

	validCommitment := make([]byte, 32)
	for i := range validCommitment {
		validCommitment[i] = byte(i)
	}

	validPromise := &PaymentPromise{
		SignerPublicKey:   pubKeyAny,
		Namespace:         validNamespace,
		BlobSize:          1000,
		Commitment:        validCommitment,
		RowVersion:        uint32(share.ShareVersionZero),
		CreationTimestamp: time.Now(),
		Signature:         []byte("valid-signature"),
		Height:            100,
		ChainId:           "celestia-test",
	}

	tests := []struct {
		name    string
		msg     *MsgPaymentPromiseTimeout
		wantErr bool
		errType error
	}{
		{
			name: "valid message",
			msg: &MsgPaymentPromiseTimeout{
				Signer:         validAddr,
				PaymentPromise: *validPromise,
			},
			wantErr: false,
		},
		{
			name: "invalid signer address",
			msg: &MsgPaymentPromiseTimeout{
				Signer:         "invalid-address",
				PaymentPromise: *validPromise,
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidAddress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.wantErr {
				require.Error(t, err)
				if tt.errType != nil {
					assert.Contains(t, err.Error(), tt.errType.Error())
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgUpdateFibreParams_ValidateBasic(t *testing.T) {
	validAddr := "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"
	validParams := DefaultParams()

	tests := []struct {
		name    string
		msg     *MsgUpdateFibreParams
		wantErr bool
		errType error
	}{
		{
			name: "valid message",
			msg: &MsgUpdateFibreParams{
				Authority: validAddr,
				Params:    validParams,
			},
			wantErr: false,
		},
		{
			name: "invalid authority address",
			msg: &MsgUpdateFibreParams{
				Authority: "invalid-address",
				Params:    validParams,
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidAddress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.wantErr {
				require.Error(t, err)
				if tt.errType != nil {
					assert.Contains(t, err.Error(), tt.errType.Error())
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func generateNamespace(t *testing.T) []byte {
	namespace, err := share.NewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	require.NoError(t, err)
	return namespace.Bytes()
}

func generateCommitment(t *testing.T) []byte {
	commitment := make([]byte, 32)
	for i := range commitment {
		commitment[i] = byte(i)
	}
	return commitment
}

func generatePubKeyAny(t *testing.T) *types.Any {
	privKey := secp256k1.GenPrivKey()
	pubKey := privKey.PubKey()
	pubKeyAny, err := types.NewAnyWithValue(pubKey)
	require.NoError(t, err)
	return pubKeyAny
}
