package app

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	blobtypes "github.com/celestiaorg/celestia-app/v11/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/stretchr/testify/require"
)

// maxFuzzBlobData bounds blob data so commitment generation stays cheap.
const maxFuzzBlobData = 4096

// txCacheFuzzFixture signs pay-for-blobs txs for the fuzz target.
type txCacheFuzzFixture struct {
	enc    encoding.Config
	signer *user.Signer
	addr   []byte
}

func newTxCacheFuzzFixture(f *testing.F) *txCacheFuzzFixture {
	f.Helper()
	enc := encoding.MakeConfig(ModuleEncodingRegisters...)
	kr := testfactory.TestKeyring(enc.Codec)
	signer, err := user.NewSigner(kr, enc.TxConfig, testfactory.ChainID, user.NewAccount(testfactory.TestAccName, 0, 0))
	require.NoError(f, err)
	return &txCacheFuzzFixture{enc: enc, signer: signer, addr: signer.Account(testfactory.TestAccName).Address()}
}

// blob builds a blob from fuzz bytes; v1 blobs are signed by the fixture's
// account so the base tx is valid.
func (fx *txCacheFuzzFixture) blob(nsID, data []byte, v1 bool) (*share.Blob, error) {
	ns, err := share.NewV0Namespace(fitBytes(nsID, share.NamespaceVersionZeroIDSize))
	if err != nil {
		return nil, err
	}
	if v1 {
		return share.NewV1Blob(ns, data, fx.addr)
	}
	return share.NewV0Blob(ns, data)
}

// signedBlobTx signs a pay-for-blobs tx for blobs and returns it as
// ProcessProposal sees it: decoded from its canonical wire encoding.
func (fx *txCacheFuzzFixture) signedBlobTx(blobs []*share.Blob) (*blobtx.BlobTx, bool) {
	raw, _, err := fx.signer.CreatePayForBlobs(testfactory.TestAccName, blobs)
	if err != nil {
		return nil, false
	}
	return roundTripBlobTx(raw)
}

// roundTripBlobTx decodes raw the way ProcessProposal does.
func roundTripBlobTx(raw []byte) (*blobtx.BlobTx, bool) {
	btx, isBlob, err := blobtx.UnmarshalBlobTx(raw)
	if err != nil || !isBlob {
		return nil, false
	}
	return btx, true
}

// fitBytes returns b truncated or right-padded with 0x01 to n bytes. The
// padding byte keeps short IDs away from the reserved all-zero namespaces.
func fitBytes(b []byte, n int) []byte {
	out := bytes.Repeat([]byte{1}, n)
	copy(out, b)
	return out
}

// sameBlobTx reports whether a and b carry identical tx bytes and identical
// blobs field by field, independently of the cache's hashing.
func sameBlobTx(a, b *blobtx.BlobTx) bool {
	if !bytes.Equal(a.Tx, b.Tx) || len(a.Blobs) != len(b.Blobs) {
		return false
	}
	for i := range a.Blobs {
		x, y := a.Blobs[i], b.Blobs[i]
		if !bytes.Equal(x.Namespace().Bytes(), y.Namespace().Bytes()) ||
			!bytes.Equal(x.Data(), y.Data()) ||
			x.ShareVersion() != y.ShareVersion() ||
			!bytes.Equal(x.Signer(), y.Signer()) {
			return false
		}
	}
	return true
}

// mutateBlobTx changes one field of base, chosen by field. It returns false
// when the mutation is not applicable or the result is not a decodable blob
// tx.
func mutateBlobTx(fx *txCacheFuzzFixture, base *blobtx.BlobTx, field uint8, pos uint16, arg []byte) (*blobtx.BlobTx, bool) {
	tx := bytes.Clone(base.Tx)
	blobs := append([]*share.Blob(nil), base.Blobs...)
	i := int(pos) % len(blobs)
	b := blobs[i]
	argByte := byte(1)
	if len(arg) > 0 && arg[0] != 0 {
		argByte = arg[0]
	}
	var err error
	switch field % 9 {
	case 0: // namespace
		var ns share.Namespace
		ns, err = share.NewV0Namespace(fitBytes(arg, share.NamespaceVersionZeroIDSize))
		if err == nil {
			blobs[i], err = share.NewBlob(ns, b.Data(), b.ShareVersion(), b.Signer())
		}
	case 1: // data, same length
		data := bytes.Clone(b.Data())
		data[int(pos)%len(data)] ^= argByte
		blobs[i], err = share.NewBlob(b.Namespace(), data, b.ShareVersion(), b.Signer())
	case 2: // data, arbitrary replacement
		blobs[i], err = share.NewBlob(b.Namespace(), arg, b.ShareVersion(), b.Signer())
	case 3: // share version
		if b.ShareVersion() == share.ShareVersionZero {
			blobs[i], err = share.NewV1Blob(b.Namespace(), b.Data(), fx.addr)
		} else {
			blobs[i], err = share.NewV0Blob(b.Namespace(), b.Data())
		}
	case 4: // signer
		blobs[i], err = share.NewV1Blob(b.Namespace(), b.Data(), fitBytes(arg, share.SignerSize))
	case 5: // inner tx byte
		tx[int(pos)%len(tx)] ^= argByte
	case 6: // inner tx replaced by the tx of another valid PFB
		other, ok := fx.signedBlobTx([]*share.Blob{b})
		if !ok {
			return nil, false
		}
		tx = other.Tx
	case 7: // blob order or count
		if len(blobs) > 1 {
			blobs[0], blobs[len(blobs)-1] = blobs[len(blobs)-1], blobs[0]
		} else {
			blobs = append(blobs, b)
		}
	case 8: // drop a blob
		if len(blobs) == 1 {
			return nil, false
		}
		blobs = append(blobs[:i], blobs[i+1:]...)
	}
	if err != nil {
		return nil, false
	}
	raw, err := blobtx.MarshalBlobTx(tx, blobs...)
	if err != nil {
		return nil, false
	}
	return roundTripBlobTx(raw)
}

// requireCachedAgrees checks that cached validation accepts btx exactly when
// full validation does.
func requireCachedAgrees(t *testing.T, app *App, btx *blobtx.BlobTx, label string) {
	t.Helper()
	fromCache, cachedErr := app.ValidateBlobTxWithCache(btx)
	fullErr := blobtypes.ValidateBlobTx(app.encodingConfig.TxConfig, btx, appconsts.SubtreeRootThreshold, appconsts.Version)
	require.Equalf(t, fullErr == nil, cachedErr == nil,
		"%s: cached validation (fromCache=%v) err=%v, full validation err=%v", label, fromCache, cachedErr, fullErr)
}

// cacheIfValid inserts btx into the cache exactly as CheckTx does: only after
// full validation succeeds.
func cacheIfValid(app *App, btx *blobtx.BlobTx) {
	if blobtypes.ValidateBlobTx(app.encodingConfig.TxConfig, btx, appconsts.SubtreeRootThreshold, appconsts.Version) == nil {
		app.txCache.Set(btx.Tx, btx.Blobs)
	}
}

// FuzzTxCacheValidation pins the CheckTx-to-ProcessProposal blob tx cache.
// ProcessProposal skips the share commitment check on a cache hit, and the
// cache is node-local, so a false hit would let a block with a bad commitment
// be accepted only by nodes that saw a colliding tx in CheckTx.
//
// The target builds a valid signed blob tx from fuzz bytes and mutates one of
// namespace, data, share version, signer, inner tx bytes or blob order/count.
// The cache populated with the base tx must hit for the mutant exactly when
// the two are field-for-field identical. Then, with a cache populated by the
// base tx, an independently signed blob tx and an arbitrary decoded blob tx
// (each inserted only if full validation passes, as in CheckTx), cached
// validation of the base and the mutant must agree with full validation.
func FuzzTxCacheValidation(f *testing.F) {
	fx := newTxCacheFuzzFixture(f)

	seedPrior, ok := func() ([]byte, bool) {
		b, err := fx.blob([]byte("prior"), []byte("prior blob data"), true)
		if err != nil {
			return nil, false
		}
		raw, _, err := fx.signer.CreatePayForBlobs(testfactory.TestAccName, []*share.Blob{b})
		return raw, err == nil
	}()
	require.True(f, ok)

	for field := range uint8(9) {
		for _, flags := range []uint8{0, 1, 2, 3} {
			f.Add([]byte("namespace"), bytes.Repeat([]byte{0xAB}, 600), flags, field, uint16(field)*7, []byte{0x5A, 0x01, 0x02}, seedPrior)
		}
	}
	f.Add([]byte{}, []byte{1}, uint8(1), uint8(4), uint16(0), bytes.Repeat([]byte{0xFF}, 20), []byte{})
	f.Add([]byte{0xFF}, []byte("x"), uint8(3), uint8(2), uint16(1), []byte("replacement data"), []byte("not a blob tx"))

	f.Fuzz(func(t *testing.T, nsID, data []byte, flags, field uint8, pos uint16, arg, prior []byte) {
		if len(data) == 0 || len(data) > maxFuzzBlobData || len(arg) > maxFuzzBlobData || len(prior) > 4*maxFuzzBlobData {
			return
		}
		first, err := fx.blob(nsID, data, flags&1 != 0)
		if err != nil {
			return
		}
		blobs := []*share.Blob{first}
		if flags&2 != 0 {
			second, err := fx.blob(append([]byte{0x02}, nsID...), append(bytes.Clone(data), 0x02), flags&1 == 0)
			if err != nil {
				return
			}
			blobs = append(blobs, second)
		}
		base, ok := fx.signedBlobTx(blobs)
		if !ok {
			return
		}
		mutant, ok := mutateBlobTx(fx, base, field, pos, arg)
		if !ok {
			return
		}

		// (a) Injectivity spot check: a lone cache entry for base hits for
		// mutant only when the two are identical.
		cache := NewTxCache()
		cache.Set(base.Tx, base.Blobs)
		require.Truef(t, cache.Exists(base.Tx, base.Blobs), "base tx must hit its own entry")
		same := sameBlobTx(base, mutant)
		require.Equalf(t, same, cache.Exists(mutant.Tx, mutant.Blobs),
			"cache hit must match field equality (field %d, identical=%v)", field%9, same)
		if bytes.Equal(base.Tx, mutant.Tx) {
			require.Equalf(t, same, cache.getBlobsHash(base.Blobs) == cache.getBlobsHash(mutant.Blobs),
				"blobs hash must match blob equality (field %d)", field%9)
		}

		// (b) Cached validation agrees with full validation under a
		// CheckTx-shaped cache population.
		app := &App{encodingConfig: fx.enc, txCache: NewTxCache()}
		cacheIfValid(app, base)
		if other, err := fx.blob(prior, append([]byte{0x03}, data...), flags&1 == 0); err == nil {
			if otherTx, ok := fx.signedBlobTx([]*share.Blob{other}); ok {
				cacheIfValid(app, otherTx)
			}
		}
		if priorTx, ok := roundTripBlobTx(prior); ok {
			cacheIfValid(app, priorTx)
			requireCachedAgrees(t, app, priorTx, "prior")
		}
		requireCachedAgrees(t, app, base, "base")
		requireCachedAgrees(t, app, mutant, "mutant")
	})
}
