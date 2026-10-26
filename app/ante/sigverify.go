package ante

import (
	"context"
	"encoding/binary"
	"runtime"
	"sync"

	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"google.golang.org/protobuf/types/known/anypb"
)

// NewCachedSigVerificationDecorator returns the SDK's signature verification
// decorator with a cache of signatures that already verified, so a signature
// checked by an earlier ante pass skips the curve operation. Everything else -
// the sequence check, the error messages, the recheck skip - is the SDK's.
func NewCachedSigVerificationDecorator(ak ante.AccountKeeper, signModeHandler *txsigning.HandlerMap, sigCache *sigcache.Cache) ante.SigVerificationDecorator {
	return ante.NewSigVerificationDecorator(ak, signModeHandler, ante.WithSignatureVerifier(cachedVerifier(sigCache)))
}

// cachedVerifier wraps authsigning.VerifySignature with a lookup in sigCache.
// The sign bytes are a pure function of the keyed inputs, so a hit skips a
// check that would have produced the same result. Only successful
// verifications are recorded, so a rejected signature is rejected again.
func cachedVerifier(sigCache *sigcache.Cache) ante.SignatureVerifier {
	return func(
		ctx context.Context,
		pubKey cryptotypes.PubKey,
		signerData txsigning.SignerData,
		sigData signing.SignatureData,
		handler *txsigning.HandlerMap,
		txData txsigning.TxData,
	) error {
		key, keyed := txSigCacheKey(pubKey, signerData, sigData, txData)
		if keyed && sigCache != nil && sigCache.Has(key) {
			return nil
		}
		if err := authsigning.VerifySignature(ctx, pubKey, signerData, sigData, handler, txData); err != nil {
			return err
		}
		if keyed && sigCache != nil {
			sigCache.Add(key)
		}
		return nil
	}
}

// PreverifyTxSignatures warms the signature cache for single-signer direct-sign txs
// before ante runs; sound because the key binds every sign-bytes input.
func PreverifyTxSignatures(ctx sdk.Context, ak ante.AccountKeeper, signModeHandler *txsigning.HandlerMap, sigCache *sigcache.Cache, txs []sdk.Tx) {
	if sigCache == nil {
		return
	}
	verify := cachedVerifier(sigCache)
	type workItem struct {
		pubKey     cryptotypes.PubKey
		signerData txsigning.SignerData
		sigData    signing.SignatureData
		txData     txsigning.TxData
	}
	// Account reads stay on this goroutine; the verify-only pool is sized by the work
	// found, up to GOMAXPROCS (NumCPU over-subscribes a CPU-limited container).
	maxWorkers := runtime.GOMAXPROCS(0)
	jobs := make(chan workItem, 2*maxWorkers)
	var (
		wg      sync.WaitGroup
		workers int
	)
	worker := func() {
		for item := range jobs {
			func() {
				defer func() { _ = recover() }()
				_ = verify(ctx, item.pubKey, item.signerData, item.sigData, signModeHandler, item.txData)
			}()
		}
	}
	defer func() {
		close(jobs)
		wg.Wait()
		_ = recover() // malformed txs remain for the ante handler
	}()
	for _, tx := range txs {
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
		acc, err := ante.GetSignerAcc(ctx, ak, signers[0])
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
		anyPk, err := codectypes.NewAnyWithValue(pubKey)
		if err != nil {
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
		if workers < maxWorkers {
			workers++
			wg.Go(worker)
		}
		jobs <- workItem{
			pubKey: pubKey,
			signerData: txsigning.SignerData{
				Address: acc.GetAddress().String(), ChainID: ctx.ChainID(),
				AccountNumber: accNum, Sequence: sigs[0].Sequence,
				PubKey: &anypb.Any{TypeUrl: anyPk.TypeUrl, Value: anyPk.Value},
			},
			sigData: sigs[0].Data,
			txData:  adaptableTx.GetSigningTxData(),
		}
	}
}

// txSigCacheKey covers every input the sign bytes are derived from, plus the
// signature itself.
//
// The key is sound only while the sign mode handler derives those bytes from
// the transaction alone. Every mode the app enables does: SIGN_MODE_DIRECT,
// DIRECT_AUX and LEGACY_AMINO_JSON all discard the context they are handed. A
// handler that reads state is excluded below and verified the ordinary way.
// Check this before enabling a new sign mode.
//
// Multi-signatures are not keyed: they are rare and each inner signature would
// need its own entry.
func txSigCacheKey(pubKey cryptotypes.PubKey, signerData txsigning.SignerData, sigData signing.SignatureData, txData txsigning.TxData) (sigcache.Key, bool) {
	single, ok := sigData.(*signing.SingleSignatureData)
	if !ok || pubKey == nil {
		return sigcache.Key{}, false
	}
	// SIGN_MODE_TEXTUAL renders coins using bank denom metadata read from
	// state, so its sign bytes are not a function of the inputs below and the
	// reads are metered. Never cache it.
	if single.SignMode == signing.SignMode_SIGN_MODE_TEXTUAL {
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
