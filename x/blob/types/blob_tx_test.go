package types_test

import (
	"bytes"
	"runtime"
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/app/params"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v6/test/util/random"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v6/test/util/testnode"
	"github.com/celestiaorg/celestia-app/v6/x/blob/types"
	"github.com/celestiaorg/go-square/v2/inclusion"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/go-square/v2/tx"
	"github.com/cometbft/cometbft/crypto/merkle"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewV0Blob(t *testing.T) {
	rawBlob := []byte{1}
	validBlob, err := types.NewV0Blob(share.RandomBlobNamespace(), rawBlob)
	require.NoError(t, err)
	require.Equal(t, validBlob.Data(), rawBlob)

	_, err = types.NewV0Blob(share.TxNamespace, rawBlob)
	require.Error(t, err)

	_, err = types.NewV0Blob(share.RandomBlobNamespace(), []byte{})
	require.Error(t, err)
}

func TestValidateBlobTx(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	signer, err := testnode.NewOfflineSigner()
	require.NoError(t, err)
	ns1 := share.MustNewV0Namespace(bytes.Repeat([]byte{0x01}, share.NamespaceVersionZeroIDSize))
	acc := signer.Account(testfactory.TestAccName)
	require.NotNil(t, acc)
	addr := acc.Address()

	type test struct {
		name        string
		getTx       func() *tx.BlobTx
		expectedErr error
	}

	validRawBtx := func() []byte {
		btx := blobfactory.RandBlobTxsWithNamespacesAndSigner(
			signer,
			[]share.Namespace{ns1},
			[]int{10},
		)[0]
		return btx
	}

	tests := []test{
		{
			name: "normal transaction",
			getTx: func() *tx.BlobTx {
				rawBtx := validRawBtx()
				btx, _, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)
				return btx
			},
			expectedErr: nil,
		},
		{
			name: "invalid transaction, mismatched namespace",
			getTx: func() *tx.BlobTx {
				rawBtx := validRawBtx()
				btx, _, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)

				originalBlob := btx.Blobs[0]
				differentBlob, err := share.NewBlob(share.RandomBlobNamespace(), originalBlob.Data(), originalBlob.ShareVersion(), originalBlob.Signer())
				require.NoError(t, err)

				btx.Blobs[0] = differentBlob
				return btx
			},
			expectedErr: types.ErrNamespaceMismatch,
		},
		{
			name: "invalid transaction, no pfb",
			getTx: func() *tx.BlobTx {
				sendTx := blobfactory.GenerateManyRawSendTxs(signer, 1)
				b, err := types.NewV0Blob(share.RandomBlobNamespace(), random.Bytes(100))
				require.NoError(t, err)
				return &tx.BlobTx{
					Tx:    sendTx[0],
					Blobs: []*share.Blob{b},
				}
			},
			expectedErr: types.ErrNoPFB,
		},
		{
			name: "mismatched number of pfbs and blobs",
			getTx: func() *tx.BlobTx {
				rawBtx := validRawBtx()
				btx, _, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)
				blob, err := types.NewV0Blob(share.RandomBlobNamespace(), random.Bytes(100))
				require.NoError(t, err)
				btx.Blobs = append(btx.Blobs, blob)
				return btx
			},
			expectedErr: types.ErrBlobSizeMismatch,
		},
		{
			name: "invalid share commitment",
			getTx: func() *tx.BlobTx {
				b, err := types.NewV0Blob(share.RandomBlobNamespace(), random.Bytes(100))
				require.NoError(t, err)
				msg, err := types.NewMsgPayForBlobs(
					addr.String(),
					appconsts.Version,
					b,
				)
				require.NoError(t, err)

				anotherBlob, err := share.NewV0Blob(share.RandomBlobNamespace(), random.Bytes(99))
				require.NoError(t, err)
				badCommit, err := inclusion.CreateCommitment(
					anotherBlob,
					merkle.HashFromByteSlices,
					appconsts.SubtreeRootThreshold,
				)
				require.NoError(t, err)

				msg.ShareCommitments[0] = badCommit

				rawTx, _, err := signer.CreateTx([]sdk.Msg{msg})
				require.NoError(t, err)

				btx := &tx.BlobTx{
					Tx:    rawTx,
					Blobs: []*share.Blob{b},
				}
				return btx
			},
			expectedErr: types.ErrInvalidShareCommitment,
		},
		{
			name: "complex transaction with one send and one pfb",
			getTx: func() *tx.BlobTx {
				sendMsg := banktypes.NewMsgSend(addr, addr, sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(10))))
				transaction := blobfactory.ComplexBlobTxWithOtherMsgs(
					t,
					random.New(),
					signer,
					sendMsg,
				)
				btx, ok, err := tx.UnmarshalBlobTx(transaction)
				require.NoError(t, err)
				require.True(t, ok)
				return btx
			},
			expectedErr: types.ErrMultipleMsgsInBlobTx,
		},
		{
			name: "only send tx",
			getTx: func() *tx.BlobTx {
				sendtx := blobfactory.GenerateManyRawSendTxs(signer, 1)[0]
				return &tx.BlobTx{
					Tx: sendtx,
				}
			},
			expectedErr: types.ErrNoPFB,
		},
		{
			name: "normal transaction with two blobs w/ different namespaces",
			getTx: func() *tx.BlobTx {
				rawBtx, _, err := signer.CreatePayForBlobs(acc.Name(),
					blobfactory.RandV0BlobsWithNamespace(
						[]share.Namespace{share.RandomBlobNamespace(), share.RandomBlobNamespace()},
						[]int{100, 100}))
				require.NoError(t, err)
				btx, isBlobTx, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)
				require.True(t, isBlobTx)
				return btx
			},
			expectedErr: nil,
		},
		{
			name: "normal transaction with two large blobs w/ different namespaces",
			getTx: func() *tx.BlobTx {
				rawBtx, _, err := signer.CreatePayForBlobs(acc.Name(),
					blobfactory.RandV0BlobsWithNamespace(
						[]share.Namespace{share.RandomBlobNamespace(), share.RandomBlobNamespace()},
						[]int{100000, 1000000},
					),
				)
				require.NoError(t, err)
				btx, isBlobTx, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)
				require.True(t, isBlobTx)
				return btx
			},
			expectedErr: nil,
		},
		{
			name: "normal transaction with two blobs w/ same namespace",
			getTx: func() *tx.BlobTx {
				ns := share.RandomBlobNamespace()
				rawBtx, _, err := signer.CreatePayForBlobs(acc.Name(),
					blobfactory.RandV0BlobsWithNamespace(
						[]share.Namespace{ns, ns},
						[]int{100, 100},
					),
				)
				require.NoError(t, err)
				btx, isBlobTx, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)
				require.True(t, isBlobTx)
				return btx
			},
			expectedErr: nil,
		},
		{
			name: "normal transaction with one hundred blobs of the same namespace",
			getTx: func() *tx.BlobTx {
				count := 100
				ns := share.RandomBlobNamespace()
				sizes := make([]int, count)
				namespaces := make([]share.Namespace, count)
				for i := 0; i < count; i++ {
					sizes[i] = 100
					namespaces[i] = ns
				}
				rawBtx, _, err := signer.CreatePayForBlobs(acc.Name(),
					blobfactory.RandV0BlobsWithNamespace(
						namespaces,
						sizes,
					))
				require.NoError(t, err)
				btx, isBlobTx, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)
				require.True(t, isBlobTx)
				return btx
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := types.ValidateBlobTx(encCfg.TxConfig, tt.getTx(), appconsts.SubtreeRootThreshold, appconsts.Version)
			if tt.expectedErr != nil {
				assert.ErrorIs(t, err, tt.expectedErr, tt.name)
			}
		})
	}
}

func TestValidateBlobTxsParallelConsistency(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	signer, err := testnode.NewOfflineSigner()
	require.NoError(t, err)
	ns1 := share.MustNewV0Namespace(bytes.Repeat([]byte{0x01}, share.NamespaceVersionZeroIDSize))
	acc := signer.Account(testfactory.TestAccName)
	require.NotNil(t, acc)

	validRawBtx := func() []byte {
		btx := blobfactory.RandBlobTxsWithNamespacesAndSigner(
			signer,
			[]share.Namespace{ns1},
			[]int{10},
		)[0]
		return btx
	}

	testCases := []struct {
		name        string
		getBlobTxs  func() []*tx.BlobTx
		expectedErr error
	}{
		{
			name: "single valid blob tx",
			getBlobTxs: func() []*tx.BlobTx {
				rawBtx := validRawBtx()
				btx, _, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)
				return []*tx.BlobTx{btx}
			},
			expectedErr: nil,
		},
		{
			name: "multiple valid blob txs",
			getBlobTxs: func() []*tx.BlobTx {
				var blobTxs []*tx.BlobTx
				for i := 0; i < 5; i++ {
					rawBtx := validRawBtx()
					btx, _, err := tx.UnmarshalBlobTx(rawBtx)
					require.NoError(t, err)
					blobTxs = append(blobTxs, btx)
				}
				return blobTxs
			},
			expectedErr: nil,
		},
		{
			name: "mixed valid and invalid blob txs - namespace mismatch",
			getBlobTxs: func() []*tx.BlobTx {
				var blobTxs []*tx.BlobTx

				rawBtx := validRawBtx()
				btx, _, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)
				blobTxs = append(blobTxs, btx)

				rawBtx2 := validRawBtx()
				btx2, _, err := tx.UnmarshalBlobTx(rawBtx2)
				require.NoError(t, err)
				originalBlob := btx2.Blobs[0]
				differentBlob, err := share.NewBlob(share.RandomBlobNamespace(), originalBlob.Data(), originalBlob.ShareVersion(), originalBlob.Signer())
				require.NoError(t, err)
				btx2.Blobs[0] = differentBlob
				blobTxs = append(blobTxs, btx2)

				return blobTxs
			},
			expectedErr: types.ErrNamespaceMismatch,
		},
		{
			name: "invalid blob tx - no pfb",
			getBlobTxs: func() []*tx.BlobTx {
				sendTx := blobfactory.GenerateManyRawSendTxs(signer, 1)
				b, err := types.NewV0Blob(share.RandomBlobNamespace(), random.Bytes(100))
				require.NoError(t, err)
				return []*tx.BlobTx{{
					Tx:    sendTx[0],
					Blobs: []*share.Blob{b},
				}}
			},
			expectedErr: types.ErrNoPFB,
		},
		{
			name: "invalid blob tx - size mismatch",
			getBlobTxs: func() []*tx.BlobTx {
				rawBtx := validRawBtx()
				btx, _, err := tx.UnmarshalBlobTx(rawBtx)
				require.NoError(t, err)
				blob, err := types.NewV0Blob(share.RandomBlobNamespace(), random.Bytes(100))
				require.NoError(t, err)
				btx.Blobs = append(btx.Blobs, blob)
				return []*tx.BlobTx{btx}
			},
			expectedErr: types.ErrBlobSizeMismatch,
		},
		{
			name: "large batch of valid txs",
			getBlobTxs: func() []*tx.BlobTx {
				var blobTxs []*tx.BlobTx
				for i := 0; i < 50; i++ {
					rawBtx := validRawBtx()
					btx, _, err := tx.UnmarshalBlobTx(rawBtx)
					require.NoError(t, err)
					blobTxs = append(blobTxs, btx)
				}
				return blobTxs
			},
			expectedErr: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			blobTxs := tc.getBlobTxs()

			serialErr := validateBlobTxsSequential(encCfg.TxConfig, blobTxs, appconsts.SubtreeRootThreshold, appconsts.Version)
			parallelErr := types.ValidateBlobTxsParallel(encCfg.TxConfig, blobTxs, appconsts.SubtreeRootThreshold, appconsts.Version, 4)

			if tc.expectedErr != nil {
				assert.ErrorIs(t, serialErr, tc.expectedErr, "serial validation should return expected error")
				assert.ErrorIs(t, parallelErr, tc.expectedErr, "parallel validation should return expected error")
			} else {
				assert.NoError(t, serialErr, "serial validation should not error")
				assert.NoError(t, parallelErr, "parallel validation should not error")
			}

			if (serialErr == nil) != (parallelErr == nil) {
				t.Errorf("validation results differ: serial error = %v, parallel error = %v", serialErr, parallelErr)
			}

			if serialErr != nil && parallelErr != nil {
				assert.Equal(t, serialErr.Error(), parallelErr.Error(), "error messages should match")
			}
		})
	}
}

func validateBlobTxsSequential(txcfg client.TxEncodingConfig, blobTxs []*tx.BlobTx, subtreeRootThreshold int, version uint64) error {
	for _, blobTx := range blobTxs {
		if err := types.ValidateBlobTx(txcfg, blobTx, subtreeRootThreshold, version); err != nil {
			return err
		}
	}
	return nil
}

func BenchmarkValidateBlobTx(b *testing.B) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	signer, err := testnode.NewOfflineSigner()
	require.NoError(b, err)
	acc := signer.Account(testfactory.TestAccName)
	require.NotNil(b, acc)

	benchmarks := []struct {
		name     string
		blobSize int
	}{
		{"300KiB", 300 * 1024},
		{"1MiB", 1024 * 1024},
		{"2MiB", 2 * 1024 * 1024},
		{"4MiB", 4 * 1024 * 1024},
		{"8MiB", 8 * 1024 * 1024},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			rawBtx, _, err := signer.CreatePayForBlobs(
				acc.Name(),
				blobfactory.RandV0BlobsWithNamespace(
					[]share.Namespace{share.RandomBlobNamespace()},
					[]int{bm.blobSize},
				),
			)
			require.NoError(b, err)

			btx, isBlobTx, err := tx.UnmarshalBlobTx(rawBtx)
			require.NoError(b, err)
			require.True(b, isBlobTx)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				err := types.ValidateBlobTx(encCfg.TxConfig, btx, appconsts.SubtreeRootThreshold, appconsts.Version)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkValidateBlobTxsComparison(b *testing.B) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	signer, err := testnode.NewOfflineSigner()
	require.NoError(b, err)
	acc := signer.Account(testfactory.TestAccName)
	require.NotNil(b, acc)

	benchmarks := []struct {
		name     string
		numTxs   int
		blobSize int
	}{
		{"100_tx_300KiB", 100, 300 * 1024},
		{"10_tx_8MiB", 10, 8 * 1024 * 1024},
		{"100_tx_1MiB", 50, 1024 * 1024},
	}

	for _, bm := range benchmarks {
		var blobTxs []*tx.BlobTx
		for i := 0; i < bm.numTxs; i++ {
			rawBtx, _, err := signer.CreatePayForBlobs(
				acc.Name(),
				blobfactory.RandV0BlobsWithNamespace(
					[]share.Namespace{share.RandomBlobNamespace()},
					[]int{bm.blobSize},
				),
			)
			require.NoError(b, err)

			btx, isBlobTx, err := tx.UnmarshalBlobTx(rawBtx)
			require.NoError(b, err)
			require.True(b, isBlobTx)
			blobTxs = append(blobTxs, btx)
		}

		b.Run(bm.name+"_sequential", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				err := validateBlobTxsSequential(encCfg.TxConfig, blobTxs, appconsts.SubtreeRootThreshold, appconsts.Version)
				if err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(bm.name+"_parallel", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				err := types.ValidateBlobTxsParallel(encCfg.TxConfig, blobTxs, appconsts.SubtreeRootThreshold, appconsts.Version, runtime.NumCPU())
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCreateCommitment(b *testing.B) {
	benchmarks := []struct {
		name     string
		blobSize int
	}{
		{"300KiB", 300 * 1024},
		{"1MiB", 1024 * 1024},
		{"2MiB", 2 * 1024 * 1024},
		{"4MiB", 4 * 1024 * 1024},
		{"8MiB", 8 * 1024 * 1024},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			blob, err := types.NewV0Blob(share.RandomBlobNamespace(), random.Bytes(bm.blobSize))
			require.NoError(b, err)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := inclusion.CreateCommitment(blob, merkle.HashFromByteSlices, appconsts.SubtreeRootThreshold)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
