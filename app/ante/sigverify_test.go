package ante

import (
	"testing"

	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/stretchr/testify/require"
)

func TestTxSigCacheKeyCoversVerificationInputs(t *testing.T) {
	pubKey := secp256k1.GenPrivKey().PubKey()
	signer := txsigning.SignerData{Address: "address", ChainID: "chain", AccountNumber: 1, Sequence: 2}
	sig := &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT, Signature: []byte("signature")}
	tx := txsigning.TxData{BodyBytes: []byte("body"), AuthInfoBytes: []byte("auth")}
	base, ok := txSigCacheKey(pubKey, signer, sig, tx)
	require.True(t, ok)

	checkDifferent := func(s txsigning.SignerData, signature *signing.SingleSignatureData, data txsigning.TxData) {
		t.Helper()
		key, keyed := txSigCacheKey(pubKey, s, signature, data)
		require.True(t, keyed)
		require.NotEqual(t, base, key)
	}
	s := signer
	s.Address = "other"
	checkDifferent(s, sig, tx)
	s = signer
	s.ChainID = "other"
	checkDifferent(s, sig, tx)
	s = signer
	s.AccountNumber++
	checkDifferent(s, sig, tx)
	s = signer
	s.Sequence++
	checkDifferent(s, sig, tx)
	checkDifferent(signer, &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT, Signature: []byte("other")}, tx)
	d := tx
	d.BodyBytes = []byte("other")
	checkDifferent(signer, sig, d)
	d = tx
	d.AuthInfoBytes = []byte("other")
	checkDifferent(signer, sig, d)
	otherKey, keyed := txSigCacheKey(secp256k1.GenPrivKey().PubKey(), signer, sig, tx)
	require.True(t, keyed)
	require.NotEqual(t, base, otherKey)
	_, keyed = txSigCacheKey(pubKey, signer, &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON, Signature: sig.Signature}, tx)
	require.False(t, keyed)
}
