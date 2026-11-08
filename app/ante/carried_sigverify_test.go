package ante

import (
	"context"
	"testing"

	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/stretchr/testify/require"
)

func TestCarriedSignatureInputs(t *testing.T) {
	pub := secp256k1.GenPrivKeyFromSecret([]byte("carried")).PubKey()
	raw := []byte("tx")
	signer := txsigning.SignerData{Address: "signer", ChainID: "chain", AccountNumber: 1, Sequence: 2}
	sig := &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT, Signature: []byte("signature")}
	data := txsigning.TxData{BodyBytes: []byte("body"), AuthInfoBytes: []byte("auth")}
	key, ok := txSigCacheKey(pub, signer, sig, data)
	require.True(t, ok)
	holder := &TxSigEvidenceHolder{Current: newTxSigEvidence(raw, key, pub, signer, sig, nil, data)}
	ctx := WithTxSigEvidenceHolder(sdk.Context{}.WithContext(context.Background()).WithTxBytes(raw), holder)
	got, ok := carriedTxSigKey(ctx, pub, signer, sig, nil, data)
	require.True(t, ok)
	require.Equal(t, key, got)
	for _, mutate := range []func(*txsigning.SignerData){func(s *txsigning.SignerData) { s.Sequence++ }, func(s *txsigning.SignerData) { s.AccountNumber++ }, func(s *txsigning.SignerData) { s.ChainID += "x" }, func(s *txsigning.SignerData) { s.Address += "x" }} {
		changed := signer
		mutate(&changed)
		_, ok := carriedTxSigKey(ctx, pub, changed, sig, nil, data)
		require.False(t, ok)
	}
	_, ok = carriedTxSigKey(ctx.WithTxBytes([]byte("other")), pub, signer, sig, nil, data)
	require.False(t, ok)
	data.BodyBytes[0] ^= 1
	_, ok = carriedTxSigKey(ctx, pub, signer, sig, nil, data)
	require.False(t, ok)
	var absent *TxSigEvidence
	holder.Current = absent
	got, ok = carriedTxSigKey(ctx, pub, signer, sig, nil, data)
	require.False(t, ok)
	require.Equal(t, sigcache.Key{}, got)
}
