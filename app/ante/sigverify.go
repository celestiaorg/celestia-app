package ante

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"

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

// PreverifyTxSignatures fills the signature cache for single-signer direct-sign
// transactions. A miss or a failed check just leaves the decision to the
// ordered ante handler.
func (d CachedSigVerificationDecorator) PreverifyTxSignatures(ctx sdk.Context, txs []sdk.Tx) {
	if d.sigCache == nil {
		return
	}
	type workItem struct {
		pubKey     cryptotypes.PubKey
		signerData txsigning.SignerData
		sigData    signing.SignatureData
		txData     txsigning.TxData
	}
	jobs := make(chan workItem, 2*runtime.NumCPU())
	var wg sync.WaitGroup
	for range min(runtime.NumCPU(), len(txs)) {
		wg.Go(func() {
			for item := range jobs {
				func() {
					defer func() { _ = recover() }()
					_ = d.verifySignature(ctx, item.pubKey, item.signerData, item.sigData, item.txData)
				}()
			}
		})
	}
	defer func() {
		close(jobs)
		wg.Wait()
		_ = recover() // malformed txs remain for the ante handler
	}()
	for _, tx := range txs {
		if tx == nil {
			continue
		}
		sigTx, ok := tx.(authsigning.Tx)
		if !ok {
			continue
		}
		sigs, err := sigTx.GetSignaturesV2()
		if err != nil || len(sigs) != 1 {
			continue
		}
		single, ok := sigs[0].Data.(*signing.SingleSignatureData)
		if !ok || single.SignMode != signing.SignMode_SIGN_MODE_DIRECT {
			continue
		}
		signers, err := sigTx.GetSigners()
		if err != nil || len(signers) != 1 {
			continue
		}
		acc, err := ante.GetSignerAcc(ctx, d.ak, signers[0])
		if err != nil {
			continue
		}
		pubKey := acc.GetPubKey()
		if pubKey == nil {
			pubKey = sigs[0].PubKey
		}
		if pubKey == nil {
			continue
		}
		var accNum uint64
		if ctx.BlockHeight() != 0 {
			accNum = acc.GetAccountNumber()
		}
		adaptableTx, ok := tx.(authsigning.V2AdaptableTx)
		if !ok {
			continue
		}
		jobs <- workItem{
			pubKey: pubKey,
			signerData: txsigning.SignerData{
				Address: acc.GetAddress().String(), ChainID: ctx.ChainID(),
				AccountNumber: accNum, Sequence: sigs[0].Sequence,
			},
			sigData: sigs[0].Data,
			txData:  adaptableTx.GetSigningTxData(),
		}
	}
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
			signerData := txsigning.SignerData{
				Address:       acc.GetAddress().String(),
				ChainID:       chainID,
				AccountNumber: accNum,
				Sequence:      acc.GetSequence(),
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
	anyPk, err := codectypes.NewAnyWithValue(pubKey)
	if err != nil {
		return err
	}
	signerData.PubKey = &anypb.Any{TypeUrl: anyPk.TypeUrl, Value: anyPk.Value}
	verificationKey := pubKey
	if _, single := sigData.(*signing.SingleSignatureData); single && ctx.ExecMode() == sdk.ExecModeProcessProposal {
		verificationKey = txVerificationPubKey(pubKey)
	}
	if err := authsigning.VerifySignature(ctx, verificationKey, signerData, sigData, d.signModeHandler, txData); err != nil {
		return err
	}
	if keyed && d.sigCache != nil {
		d.sigCache.Add(key)
	}
	return nil
}

// keyableSignModes are the sign modes whose sign bytes are a pure function of
// the inputs txSigCacheKey covers. SIGN_MODE_TEXTUAL is deliberately absent: its
// handler reads bank denom metadata from state, so a cached result would both
// skip state-dependent sign bytes and skip the reads that pay for them. A sign
// mode enabled later must be added here consciously rather than inherited.
var keyableSignModes = map[signing.SignMode]struct{}{
	signing.SignMode_SIGN_MODE_DIRECT:            {},
	signing.SignMode_SIGN_MODE_DIRECT_AUX:        {},
	signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON: {},
}

// txSigCacheKey covers every input the sign bytes are derived from, plus the
// signature itself. Multi-signatures are not keyed: they are rare and each
// inner signature would need its own entry.
func txSigCacheKey(pubKey cryptotypes.PubKey, signerData txsigning.SignerData, sigData signing.SignatureData, txData txsigning.TxData) (sigcache.Key, bool) {
	single, ok := sigData.(*signing.SingleSignatureData)
	if !ok || pubKey == nil {
		return sigcache.Key{}, false
	}
	if _, ok := keyableSignModes[single.SignMode]; !ok {
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
