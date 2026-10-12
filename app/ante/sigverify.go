package ante

import (
	"context"
	"encoding/binary"

	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
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

// txSigCacheKey covers every input the sign bytes are derived from, plus the
// signature itself.
//
// This assumes the sign mode handler derives those bytes from the transaction
// alone. Every enabled mode does: SIGN_MODE_DIRECT, DIRECT_AUX and
// LEGACY_AMINO_JSON all discard the context they are handed. A handler that
// reads state must not be cached here - SIGN_MODE_TEXTUAL queries bank denom
// metadata, so the key would not see a metadata change, and skipping the reads
// would make gas differ between a warm and a cold node. Check this before
// enabling a new sign mode. Nothing guards it at runtime: a mode that fails to
// verify is simply never admitted, since the cache holds successes only.
//
// Multi-signatures are not keyed: they are rare and each inner signature would
// need its own entry.
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
