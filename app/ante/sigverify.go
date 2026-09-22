package ante

import (
	"encoding/binary"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"google.golang.org/protobuf/types/known/anypb"
)

var _ sdk.AnteDecorator = CachedSigVerificationDecorator{}

// CachedSigVerificationDecorator is the SDK's SigVerificationDecorator with a
// cache of signatures that already verified. It replaces the SDK decorator in
// the chain, so the sequence check, the error messages and the recheck skip are
// deliberate copies. Keep them in sync when bumping the SDK.
type CachedSigVerificationDecorator struct {
	ak              ante.AccountKeeper
	signModeHandler *txsigning.HandlerMap
	sigCache        *sigcache.Cache
}

// NewCachedSigVerificationDecorator returns a caching signature verification decorator.
func NewCachedSigVerificationDecorator(ak ante.AccountKeeper, signModeHandler *txsigning.HandlerMap, sigCache *sigcache.Cache) CachedSigVerificationDecorator {
	return CachedSigVerificationDecorator{ak: ak, signModeHandler: signModeHandler, sigCache: sigCache}
}

func (d CachedSigVerificationDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	sigTx, ok := tx.(authsigning.Tx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "invalid transaction type")
	}

	sigs, err := sigTx.GetSignaturesV2()
	if err != nil {
		return ctx, err
	}

	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, err
	}

	if len(sigs) != len(signers) {
		return ctx, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "invalid number of signer;  expected: %d, got %d", len(signers), len(sigs))
	}

	for i, sig := range sigs {
		acc, err := ante.GetSignerAcc(ctx, d.ak, signers[i])
		if err != nil {
			return ctx, err
		}

		pubKey := acc.GetPubKey()
		if !simulate && pubKey == nil {
			return ctx, errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "pubkey on account is not set")
		}

		if sig.Sequence != acc.GetSequence() {
			return ctx, errorsmod.Wrapf(
				sdkerrors.ErrWrongSequence,
				"account sequence mismatch, expected %d, got %d", acc.GetSequence(), sig.Sequence,
			)
		}

		genesis := ctx.BlockHeight() == 0
		chainID := ctx.ChainID()
		var accNum uint64
		if !genesis {
			accNum = acc.GetAccountNumber()
		}

		// no need to verify signatures on recheck tx
		if !simulate && !ctx.IsReCheckTx() && ctx.IsSigverifyTx() {
			anyPk, _ := codectypes.NewAnyWithValue(pubKey)

			signerData := txsigning.SignerData{
				Address:       acc.GetAddress().String(),
				ChainID:       chainID,
				AccountNumber: accNum,
				Sequence:      acc.GetSequence(),
				PubKey: &anypb.Any{
					TypeUrl: anyPk.TypeUrl,
					Value:   anyPk.Value,
				},
			}
			adaptableTx, ok := tx.(authsigning.V2AdaptableTx)
			if !ok {
				return ctx, fmt.Errorf("expected tx to implement V2AdaptableTx, got %T", tx)
			}
			txData := adaptableTx.GetSigningTxData()
			if err := d.verifySignature(ctx, pubKey, signerData, sig.Data, txData); err != nil {
				var errMsg string
				if ante.OnlyLegacyAminoSigners(sig.Data) {
					// If all signers are using SIGN_MODE_LEGACY_AMINO, we rely on VerifySignature to check account sequence number,
					// and therefore communicate sequence number as a potential cause of error.
					errMsg = fmt.Sprintf("signature verification failed; please verify account number (%d), sequence (%d) and chain-id (%s)", accNum, acc.GetSequence(), chainID)
				} else {
					errMsg = fmt.Sprintf("signature verification failed; please verify account number (%d) and chain-id (%s): (%s)", accNum, chainID, err.Error())
				}
				return ctx, errorsmod.Wrap(sdkerrors.ErrUnauthorized, errMsg)
			}
		}
	}

	return next(ctx, tx, simulate)
}

// verifySignature skips the curve operation when the same signature already
// verified. The sign bytes are a pure function of the key inputs below, so a
// hit skips a check that would have produced the same result.
func (d CachedSigVerificationDecorator) verifySignature(
	ctx sdk.Context,
	pubKey cryptotypes.PubKey,
	signerData txsigning.SignerData,
	sigData signing.SignatureData,
	txData txsigning.TxData,
) error {
	key, keyed := txSigCacheKey(pubKey, signerData, sigData, txData)
	if keyed && d.sigCache != nil && d.sigCache.Has(key) {
		return nil
	}
	if err := authsigning.VerifySignature(ctx, pubKey, signerData, sigData, d.signModeHandler, txData); err != nil {
		return err
	}
	if keyed && d.sigCache != nil {
		d.sigCache.Add(key)
	}
	return nil
}

// txSigCacheKey covers every input the sign bytes are derived from, plus the
// signature itself. Multi-signatures are not keyed: they are rare and each
// inner signature would need its own entry.
func txSigCacheKey(pubKey cryptotypes.PubKey, signerData txsigning.SignerData, sigData signing.SignatureData, txData txsigning.TxData) (sigcache.Key, bool) {
	single, ok := sigData.(*signing.SingleSignatureData)
	if !ok || pubKey == nil {
		return sigcache.Key{}, false
	}

	var scalars [21]byte
	binary.BigEndian.PutUint64(scalars[0:8], signerData.AccountNumber)
	binary.BigEndian.PutUint64(scalars[8:16], signerData.Sequence)
	binary.BigEndian.PutUint32(scalars[16:20], uint32(single.SignMode))
	if txData.BodyHasUnknownNonCriticals {
		scalars[20] = 1
	}

	return sigcache.NewKey(sigcache.TxSignature,
		[]byte(pubKey.Type()),
		pubKey.Bytes(),
		[]byte(signerData.Address),
		[]byte(signerData.ChainID),
		scalars[:],
		txData.BodyBytes,
		txData.AuthInfoBytes,
		single.Signature,
	), true
}
