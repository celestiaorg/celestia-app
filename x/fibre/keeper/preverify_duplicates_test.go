package keeper_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/keeper"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cosmostx "github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

type observedSignatureCache struct {
	*sigcache.Cache
	adds map[sigcache.Key]int
}

func (c *observedSignatureCache) Add(key sigcache.Key) {
	c.adds[key]++
	c.Cache.Add(key)
}

func TestPreverifySignaturesDeduplicatesCertificates(t *testing.T) {
	f := newPreverifyFixture(t)
	observedCache := &observedSignatureCache{Cache: sigcache.New(1024), adds: make(map[sigcache.Key]int)}
	k := keeper.NewKeeper(codec.NewProtoCodec(codectypes.NewInterfaceRegistry()), f.storeKey,
		&MockBankKeeper{}, f.stakingKeeper, authtypes.NewModuleAddress("gov").String(), false, observedCache)
	duplicates := appconsts.MaxPayForFibreMessages + 1
	raw := f.txBytes(t)
	txs := make([][]byte, duplicates)
	for i := range txs {
		txs[i] = raw
	}
	k.PreverifySignatures(f.ctx, txs, keeper.PreverifyOptions{Certificates: true, StopOnFirstFailure: true})
	require.Equal(t, 1, observedCache.adds[f.certKey], "each certificate must be verified once per pass")
}

func TestPreverifySignaturesBoundsCertificateWork(t *testing.T) {
	f := newPreverifyFixture(t)
	txs := make([][]byte, appconsts.MaxPayForFibreMessages+1)
	keys := make([]sigcache.Key, len(txs))
	for i := range txs {
		// The fourth signature is outside the accepted quorum prefix. Varying
		// it gives distinct, valid certificates for the same payment promise.
		f.msg.ValidatorSignatures[3] = []byte{byte(i), byte(i >> 8)}
		f.refreshCertKey(t)
		txs[i], keys[i] = f.txBytes(t), f.certKey
	}
	f.keeper.PreverifySignatures(f.ctx, txs, keeper.PreverifyOptions{Certificates: true})
	for _, key := range keys[:appconsts.MaxPayForFibreMessages] {
		require.True(t, f.cache.Has(key))
	}
	require.False(t, f.cache.Has(keys[len(keys)-1]), "work past the per-block PFF limit must remain uncached")
}

func TestPreverifySignaturesSkipsInsufficientGas(t *testing.T) {
	f := newPreverifyFixture(t)
	var tx cosmostx.TxRaw
	require.NoError(t, tx.Unmarshal(f.txBytes(t)))
	authInfo, err := (&cosmostx.AuthInfo{Fee: &cosmostx.Fee{GasLimit: 0}}).Marshal()
	require.NoError(t, err)
	tx.AuthInfoBytes = authInfo
	raw, err := tx.Marshal()
	require.NoError(t, err)
	f.keeper.PreverifySignatures(f.ctx, [][]byte{raw}, keeper.PreverifyOptions{Certificates: true})
	require.Zero(t, f.cache.Len(), "a transaction without signature gas must not warm any signature cache")
}
