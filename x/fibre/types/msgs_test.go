package types

import (
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

func TestMsgDepositToEscrow_ValidateBasic(t *testing.T) {
	validAddr := "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"
	validCoin := sdk.NewCoin("utia", math.NewInt(1000))
	zeroCoin := sdk.NewCoin("utia", math.NewInt(0))

	tests := []struct {
		name    string
		msg     *MsgDepositToEscrow
		wantErr bool
		errType error
	}{
		{
			name: "valid message",
			msg: &MsgDepositToEscrow{
				Signer: validAddr,
				Amount: validCoin,
			},
			wantErr: false,
		},
		{
			name: "invalid signer address",
			msg: &MsgDepositToEscrow{
				Signer: "invalid-address",
				Amount: validCoin,
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidAddress,
		},
		{
			name: "empty signer address",
			msg: &MsgDepositToEscrow{
				Signer: "",
				Amount: validCoin,
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidAddress,
		},
		{
			name: "zero amount",
			msg: &MsgDepositToEscrow{
				Signer: validAddr,
				Amount: zeroCoin,
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidCoins,
		},
		{
			name: "negative amount",
			msg: &MsgDepositToEscrow{
				Signer: validAddr,
				Amount: sdk.Coin{Denom: "utia", Amount: math.NewInt(-100)},
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidCoins,
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

func TestMsgRequestWithdrawal_ValidateBasic(t *testing.T) {
	validAddr := "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"
	validCoin := sdk.NewCoin("utia", math.NewInt(1000))
	zeroCoin := sdk.NewCoin("utia", math.NewInt(0))

	tests := []struct {
		name    string
		msg     *MsgRequestWithdrawal
		wantErr bool
		errType error
	}{
		{
			name: "valid message",
			msg: &MsgRequestWithdrawal{
				Signer: validAddr,
				Amount: validCoin,
			},
			wantErr: false,
		},
		{
			name: "invalid signer address",
			msg: &MsgRequestWithdrawal{
				Signer: "invalid-address",
				Amount: validCoin,
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidAddress,
		},
		{
			name: "zero amount",
			msg: &MsgRequestWithdrawal{
				Signer: validAddr,
				Amount: zeroCoin,
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidCoins,
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

func TestPaymentPromise_ValidateBasic(t *testing.T) {
	// Create a valid public key
	privKey := secp256k1.GenPrivKey()
	pubKey := privKey.PubKey()
	pubKeyAny, err := types.NewAnyWithValue(pubKey)
	require.NoError(t, err)

	// Create a valid namespace (version 0 with 18 leading zeros)
	validNamespace := make([]byte, share.NamespaceSize)
	validNamespace[0] = 0 // version 0
	// bytes 1-18 are already zero (18 leading zeros for version 0)
	// Set bytes 19-28 to create a valid user namespace
	for i := 19; i < share.NamespaceSize; i++ {
		validNamespace[i] = 1
	}

	validCommitment := make([]byte, 32)
	for i := range validCommitment {
		validCommitment[i] = byte(i)
	}

	tests := []struct {
		name    string
		msg     *PaymentPromise
		wantErr bool
		errType error
	}{
		{
			name: "valid payment promise",
			msg: &PaymentPromise{
				SignerPublicKey:   pubKeyAny,
				Namespace:         validNamespace,
				BlobSize:          1000,
				Commitment:        validCommitment,
				RowVersion:        uint32(share.ShareVersionZero),
				CreationTimestamp: time.Now(),
				Signature:         []byte("valid-signature"),
				Height:            100,
				ChainId:           "celestia-test",
			},
			wantErr: false,
		},
		{
			name: "nil public key",
			msg: &PaymentPromise{
				SignerPublicKey:   nil,
				Namespace:         validNamespace,
				BlobSize:          1000,
				Commitment:        validCommitment,
				RowVersion:        uint32(share.ShareVersionZero),
				CreationTimestamp: time.Now(),
				Signature:         []byte("valid-signature"),
				Height:            100,
				ChainId:           "celestia-test",
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidPubKey,
		},
		{
			name: "empty namespace",
			msg: &PaymentPromise{
				SignerPublicKey:   pubKeyAny,
				Namespace:         []byte{},
				BlobSize:          1000,
				Commitment:        validCommitment,
				RowVersion:        uint32(share.ShareVersionZero),
				CreationTimestamp: time.Now(),
				Signature:         []byte("valid-signature"),
				Height:            100,
				ChainId:           "celestia-test",
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "invalid namespace size",
			msg: &PaymentPromise{
				SignerPublicKey:   pubKeyAny,
				Namespace:         []byte{1, 2, 3}, // wrong size
				BlobSize:          1000,
				Commitment:        validCommitment,
				RowVersion:        uint32(share.ShareVersionZero),
				CreationTimestamp: time.Now(),
				Signature:         []byte("valid-signature"),
				Height:            100,
				ChainId:           "celestia-test",
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "zero blob size",
			msg: &PaymentPromise{
				SignerPublicKey:   pubKeyAny,
				Namespace:         validNamespace,
				BlobSize:          0,
				Commitment:        validCommitment,
				RowVersion:        uint32(share.ShareVersionZero),
				CreationTimestamp: time.Now(),
				Signature:         []byte("valid-signature"),
				Height:            100,
				ChainId:           "celestia-test",
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "invalid commitment size",
			msg: &PaymentPromise{
				SignerPublicKey:   pubKeyAny,
				Namespace:         validNamespace,
				BlobSize:          1000,
				Commitment:        []byte{1, 2, 3}, // wrong size
				RowVersion:        uint32(share.ShareVersionZero),
				CreationTimestamp: time.Now(),
				Signature:         []byte("valid-signature"),
				Height:            100,
				ChainId:           "celestia-test",
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "unsupported row version",
			msg: &PaymentPromise{
				SignerPublicKey:   pubKeyAny,
				Namespace:         validNamespace,
				BlobSize:          1000,
				Commitment:        validCommitment,
				RowVersion:        999, // unsupported version
				CreationTimestamp: time.Now(),
				Signature:         []byte("valid-signature"),
				Height:            100,
				ChainId:           "celestia-test",
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "zero height",
			msg: &PaymentPromise{
				SignerPublicKey:   pubKeyAny,
				Namespace:         validNamespace,
				BlobSize:          1000,
				Commitment:        validCommitment,
				RowVersion:        uint32(share.ShareVersionZero),
				CreationTimestamp: time.Now(),
				Signature:         []byte("valid-signature"),
				Height:            0,
				ChainId:           "celestia-test",
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "empty signature",
			msg: &PaymentPromise{
				SignerPublicKey:   pubKeyAny,
				Namespace:         validNamespace,
				BlobSize:          1000,
				Commitment:        validCommitment,
				RowVersion:        uint32(share.ShareVersionZero),
				CreationTimestamp: time.Now(),
				Signature:         []byte{},
				Height:            100,
				ChainId:           "celestia-test",
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "empty chain ID",
			msg: &PaymentPromise{
				SignerPublicKey:   pubKeyAny,
				Namespace:         validNamespace,
				BlobSize:          1000,
				Commitment:        validCommitment,
				RowVersion:        uint32(share.ShareVersionZero),
				CreationTimestamp: time.Now(),
				Signature:         []byte("valid-signature"),
				Height:            100,
				ChainId:           "",
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
		msg     *MsgPayForFibre
		wantErr bool
		errType error
	}{
		{
			name: "valid message",
			msg: &MsgPayForFibre{
				Signer:              validAddr,
				PaymentPromise:      *validPromise,
				ValidatorSignatures: [][]byte{[]byte("sig1"), []byte("sig2")},
			},
			wantErr: false,
		},
		{
			name: "invalid signer address",
			msg: &MsgPayForFibre{
				Signer:              "invalid-address",
				PaymentPromise:      *validPromise,
				ValidatorSignatures: [][]byte{[]byte("sig1")},
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidAddress,
		},
		{
			name: "no validator signatures",
			msg: &MsgPayForFibre{
				Signer:              validAddr,
				PaymentPromise:      *validPromise,
				ValidatorSignatures: [][]byte{},
			},
			wantErr: true,
			errType: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "empty validator signature",
			msg: &MsgPayForFibre{
				Signer:              validAddr,
				PaymentPromise:      *validPromise,
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
