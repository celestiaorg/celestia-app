package ante

import (
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/stretchr/testify/require"
)

// pubKeyTx is a SigVerifiableTx that only answers GetPubKeys, the one method
// PubKeySizeDecorator calls.
type pubKeyTx struct {
	authsigning.SigVerifiableTx
	pubKeys []cryptotypes.PubKey
}

func (tx pubKeyTx) GetPubKeys() ([]cryptotypes.PubKey, error) { return tx.pubKeys, nil }

func TestPubKeySizeDecorator(t *testing.T) {
	secp := secp256k1.GenPrivKey().PubKey()
	ed := ed25519.GenPrivKey().PubKey()
	validMultisig := multisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{secp, secp256k1.GenPrivKey().PubKey()})
	badMultisig := multisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{secp, &secp256k1.PubKey{Key: []byte{2}}})
	nilMember := &multisig.LegacyAminoPubKey{Threshold: 1, PubKeys: []*codectypes.Any{nil}}
	r1Priv, err := secp256r1.GenPrivKey()
	require.NoError(t, err)
	r1 := r1Priv.PubKey()
	emptyMember := &multisig.LegacyAminoPubKey{Threshold: 1, PubKeys: []*codectypes.Any{{}}}
	// The multisig's Address amino-encodes its members, and amino cannot
	// encode a secp256r1 key.
	r1Member := multisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{secp, r1})
	nestedMultisig := multisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{validMultisig, ed})

	testCases := []struct {
		name    string
		pubKeys []cryptotypes.PubKey
		wantErr bool
	}{
		{"valid secp256k1", []cryptotypes.PubKey{secp}, false},
		{"valid ed25519", []cryptotypes.PubKey{ed}, false},
		{"omitted key", []cryptotypes.PubKey{nil}, false},
		{"valid multisig", []cryptotypes.PubKey{validMultisig}, false},
		{"valid secp256r1", []cryptotypes.PubKey{r1}, false},
		{"empty secp256r1", []cryptotypes.PubKey{&secp256r1.PubKey{}}, true},
		{"short secp256k1", []cryptotypes.PubKey{&secp256k1.PubKey{Key: []byte("0")}}, true},
		{"long secp256k1", []cryptotypes.PubKey{&secp256k1.PubKey{Key: make([]byte, secp256k1.PubKeySize+1)}}, true},
		{"empty secp256k1", []cryptotypes.PubKey{&secp256k1.PubKey{}}, true},
		{"short ed25519", []cryptotypes.PubKey{&ed25519.PubKey{Key: make([]byte, ed25519.PubKeySize-1)}}, true},
		{"second signer wrong", []cryptotypes.PubKey{secp, &secp256k1.PubKey{Key: []byte{1}}}, true},
		{"multisig member wrong", []cryptotypes.PubKey{badMultisig}, true},
		{"multisig nil member", []cryptotypes.PubKey{nilMember}, true},
		{"multisig member not a key", []cryptotypes.PubKey{emptyMember}, true},
		{"multisig secp256r1 member", []cryptotypes.PubKey{r1Member}, true},
		{"nested multisig", []cryptotypes.PubKey{nestedMultisig}, false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
				called = true
				return ctx, nil
			}
			var err error
			require.NotPanics(t, func() {
				_, err = NewPubKeySizeDecorator().AnteHandle(sdk.Context{}, pubKeyTx{pubKeys: tc.pubKeys}, false, next)
			})
			if tc.wantErr {
				require.ErrorIs(t, err, sdkerrors.ErrInvalidPubKey)
				require.False(t, called)
				return
			}
			require.NoError(t, err)
			require.True(t, called)
			// SetPubKeyDecorator calls Address on every accepted key next.
			for _, pk := range tc.pubKeys {
				if pk != nil {
					require.NotPanics(t, func() { pk.Address() })
				}
			}
		})
	}
}

// TestPubKeySizeDecoratorSimulate checks that a simulation may carry the empty
// secp256k1 key the client puts in when it has no keybase, and nothing else.
func TestPubKeySizeDecoratorSimulate(t *testing.T) {
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }
	run := func(pk cryptotypes.PubKey, simulate bool) error {
		_, err := NewPubKeySizeDecorator().AnteHandle(sdk.Context{}, pubKeyTx{pubKeys: []cryptotypes.PubKey{pk}}, simulate, next)
		return err
	}
	require.NoError(t, run(&secp256k1.PubKey{}, true))
	require.ErrorIs(t, run(&secp256k1.PubKey{}, false), sdkerrors.ErrInvalidPubKey)
	require.ErrorIs(t, run(&secp256k1.PubKey{Key: []byte("0")}, true), sdkerrors.ErrInvalidPubKey)
	require.ErrorIs(t, run(&ed25519.PubKey{}, true), sdkerrors.ErrInvalidPubKey)
}
