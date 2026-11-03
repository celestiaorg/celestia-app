package ante

import (
	errorsmod "cosmossdk.io/errors"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

var _ sdk.AnteDecorator = PubKeySizeDecorator{}

// PubKeySizeDecorator rejects a tx whose signer infos carry a secp256k1 or
// ed25519 public key of the wrong length or an empty secp256r1 public key,
// including keys inside a multisig.
// The SDK's SetPubKeyDecorator calls PubKey.Address, which panics on such a
// key, so this decorator must run before it.
type PubKeySizeDecorator struct{}

func NewPubKeySizeDecorator() PubKeySizeDecorator {
	return PubKeySizeDecorator{}
}

func (PubKeySizeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "invalid tx type")
	}
	pubKeys, err := sigTx.GetPubKeys()
	if err != nil {
		return ctx, err
	}
	for i, pk := range pubKeys {
		if err := validatePubKeySize(pk); err != nil {
			return ctx, errorsmod.Wrapf(sdkerrors.ErrInvalidPubKey, "signer index %d: %s", i, err)
		}
	}
	return next(ctx, tx, simulate)
}

// validatePubKeySize returns an error if pk, or any key nested in a multisig
// pk, is a secp256k1 or ed25519 key of the wrong length or an empty secp256r1
// key, or if a multisig
// member is not a public key. A nil pk (omitted because the account already
// has one) is valid.
func validatePubKeySize(pk cryptotypes.PubKey) error {
	switch k := pk.(type) {
	case nil:
		return nil
	case *secp256k1.PubKey:
		if k == nil || len(k.Key) != secp256k1.PubKeySize {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidPubKey, "secp256k1 public key must be %d bytes", secp256k1.PubKeySize)
		}
	case *ed25519.PubKey:
		if k == nil || len(k.Key) != ed25519.PubKeySize {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidPubKey, "ed25519 public key must be %d bytes", ed25519.PubKeySize)
		}
	case *secp256r1.PubKey:
		// Unmarshal checks the length of a non-empty key, but an empty key
		// leaves Key nil and Address dereferences it.
		if k == nil || k.Key == nil {
			return errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "empty secp256r1 public key")
		}
	case *multisig.LegacyAminoPubKey:
		if k == nil {
			return errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "nil multisig public key")
		}
		// Read the members directly: GetPubKeys panics on a member that did
		// not unpack to a public key.
		for _, member := range k.PubKeys {
			if member == nil {
				return errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "nil multisig member")
			}
			memberKey, ok := member.GetCachedValue().(cryptotypes.PubKey)
			if !ok {
				return errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "multisig member is not a public key")
			}
			if err := validatePubKeySize(memberKey); err != nil {
				return err
			}
		}
	}
	return nil
}
