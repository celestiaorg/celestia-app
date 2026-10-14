package app

import (
	"bytes"
	"fmt"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	"github.com/celestiaorg/celestia-app/v10/test/util/random"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	blobtypes "github.com/celestiaorg/celestia-app/v10/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blobTxVector is a blob tx and the error stateless validation must return
// (nil for a valid tx).
type blobTxVector struct {
	name    string
	btx     *blobtx.BlobTx
	wantErr error
}

// blobTxCacheVectors returns a valid blob tx followed by mutations of its tx
// bytes, blob data, namespaces, sizes, share versions and signers.
func blobTxCacheVectors(t *testing.T) (*App, []blobTxVector) {
	t.Helper()
	enc := encoding.MakeConfig(ModuleEncodingRegisters...)
	kr := testfactory.TestKeyring(enc.Codec)
	signer, err := user.NewSigner(kr, enc.TxConfig, testfactory.ChainID, user.NewAccount(testfactory.TestAccName, 0, 0))
	require.NoError(t, err)
	addr := signer.Account(testfactory.TestAccName).Address()
	ns1 := share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	ns2 := share.MustNewV0Namespace(bytes.Repeat([]byte{2}, share.NamespaceVersionZeroIDSize))
	ns3 := share.MustNewV0Namespace(bytes.Repeat([]byte{3}, share.NamespaceVersionZeroIDSize))

	data1, data2 := random.Bytes(100), random.Bytes(200)
	blob1, err := share.NewV1Blob(ns1, data1, addr)
	require.NoError(t, err)
	blob2, err := share.NewV0Blob(ns2, data2)
	require.NoError(t, err)
	rawTx, _, err := signer.CreatePayForBlobs(testfactory.TestAccName, []*share.Blob{blob1, blob2})
	require.NoError(t, err)
	valid, isBlob, err := blobtx.UnmarshalBlobTx(rawTx)
	require.NoError(t, err)
	require.True(t, isBlob)

	withBlobs := func(blobs ...*share.Blob) *blobtx.BlobTx {
		return &blobtx.BlobTx{Tx: valid.Tx, Blobs: blobs}
	}
	newBlob := func(ns share.Namespace, data []byte, version uint8, signer []byte) *share.Blob {
		b, err := share.NewBlob(ns, data, version, signer)
		require.NoError(t, err)
		return b
	}
	otherAddr := bytes.Repeat([]byte{0xAA}, share.SignerSize)
	flippedData := append([]byte{^data1[0]}, data1[1:]...)

	// Same blobs, but the signed PFB commits to blob2's commitment for blob1.
	badCommitMsg, err := blobtypes.NewMsgPayForBlobs(addr.String(), appconsts.Version, blob1, blob2)
	require.NoError(t, err)
	badCommitMsg.ShareCommitments[0] = badCommitMsg.ShareCommitments[1]
	badCommitTx, _, err := signer.CreateTx([]sdk.Msg{badCommitMsg})
	require.NoError(t, err)

	// Flipping the last byte changes the signature but no field checked here.
	flippedSigTx := bytes.Clone(valid.Tx)
	flippedSigTx[len(flippedSigTx)-1] ^= 0xFF

	app := &App{encodingConfig: enc, txCache: NewTxCache()}
	return app, []blobTxVector{
		{name: "valid", btx: valid},
		{name: "tx bytes: flipped signature byte", btx: &blobtx.BlobTx{Tx: flippedSigTx, Blobs: valid.Blobs}},
		{name: "tx bytes: wrong share commitment", btx: &blobtx.BlobTx{Tx: badCommitTx, Blobs: valid.Blobs}, wantErr: blobtypes.ErrInvalidShareCommitment},
		{name: "blob data: same size", btx: withBlobs(newBlob(ns1, flippedData, share.ShareVersionOne, addr), blob2), wantErr: blobtypes.ErrInvalidShareCommitment},
		{name: "blob data: longer", btx: withBlobs(newBlob(ns1, append(bytes.Clone(data1), 0), share.ShareVersionOne, addr), blob2), wantErr: blobtypes.ErrBlobSizeMismatch},
		{name: "blob namespace", btx: withBlobs(newBlob(ns3, data1, share.ShareVersionOne, addr), blob2), wantErr: blobtypes.ErrNamespaceMismatch},
		{name: "blob share version", btx: withBlobs(newBlob(ns1, data1, share.ShareVersionZero, nil), blob2), wantErr: blobtypes.ErrShareVersionMismatch},
		{name: "blob signer", btx: withBlobs(newBlob(ns1, data1, share.ShareVersionOne, otherAddr), blob2), wantErr: blobtypes.ErrInvalidBlobSigner},
		{name: "blobs reordered", btx: withBlobs(blob2, blob1), wantErr: blobtypes.ErrBlobSizeMismatch},
		{name: "blob dropped", btx: withBlobs(blob1), wantErr: blobtypes.ErrBlobSizeMismatch},
		{name: "blob appended", btx: withBlobs(blob1, blob2, blob2), wantErr: blobtypes.ErrBlobSizeMismatch},
	}
}

// TestBlobTxCacheValidationStates runs each vector through a cold, warm and
// cleared cache and checks the result does not depend on the cache state.
func TestBlobTxCacheValidationStates(t *testing.T) {
	app, vectors := blobTxCacheVectors(t)
	valid := vectors[0].btx

	states := []struct {
		name    string
		prepare func()
	}{
		{name: "cold", prepare: func() {}},
		// CheckTx caches only the valid tx.
		{name: "warm", prepare: func() { app.txCache.Set(valid.Tx, valid.Blobs) }},
		// Filling the cache evicts every earlier entry.
		{name: "cleared", prepare: func() {
			for i := range defaultTxCacheCapacity {
				app.txCache.Set(fmt.Appendf(nil, "filler-%d", i), nil)
			}
		}},
	}

	for _, state := range states {
		state.prepare()
		for _, v := range vectors {
			t.Run(state.name+"/"+v.name, func(t *testing.T) {
				fromCache, err := app.ValidateBlobTxWithCache(v.btx)
				require.ErrorIs(t, err, v.wantErr)
				assert.Equal(t, state.name == "warm" && v.btx == valid, fromCache)
			})
		}
	}
}

// TestBlobTxCacheHitKeepsStructuralChecks caches each vector as-is and checks
// that a cache hit only skips the share commitment check.
func TestBlobTxCacheHitKeepsStructuralChecks(t *testing.T) {
	app, vectors := blobTxCacheVectors(t)
	for _, v := range vectors {
		if v.wantErr == blobtypes.ErrInvalidShareCommitment {
			continue
		}
		t.Run(v.name, func(t *testing.T) {
			app.txCache.Set(v.btx.Tx, v.btx.Blobs)
			fromCache, err := app.ValidateBlobTxWithCache(v.btx)
			require.True(t, fromCache)
			require.ErrorIs(t, err, v.wantErr)
		})
	}
}

// TestBlobTxCacheConcurrentValidation validates every vector while other
// goroutines write to the cache, and checks results never change.
func TestBlobTxCacheConcurrentValidation(t *testing.T) {
	app, vectors := blobTxCacheVectors(t)
	valid := vectors[0].btx

	var wg sync.WaitGroup
	for w := range 4 {
		wg.Go(func() {
			for i := range 50 {
				app.txCache.Set(valid.Tx, valid.Blobs)
				app.txCache.Set(fmt.Appendf(nil, "filler-%d-%d", w, i), valid.Blobs)
			}
		})
	}
	for _, v := range vectors {
		wg.Go(func() {
			for range 5 {
				_, err := app.ValidateBlobTxWithCache(v.btx)
				assert.ErrorIs(t, err, v.wantErr, v.name)
			}
		})
	}
	wg.Wait()
}
