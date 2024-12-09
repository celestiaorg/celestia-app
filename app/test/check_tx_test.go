package app_test

import (
	"bytes"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	apperr "github.com/celestiaorg/celestia-app/v3/app/errors"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/go-square/v2/tx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// Here we only need to check the functionality that is added to CheckTx. We
// assume that the rest of CheckTx is tested by the cosmos-sdk.
func TestCheckTx(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	ns1, err := share.NewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	require.NoError(t, err)

	accs := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"}

	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accs...)
	testApp.Commit()

	opts := blobfactory.FeeTxOpts(1e9)

	type test struct {
		name             string
		checkType        abci.CheckTxType
		getTx            func() []byte
		expectedABCICode uint32
		expectedLog      string
	}

	tests := []test{
		{
			name:      "normal transaction, CheckTxType_New",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := createSigner(t, kr, accs[0], encCfg.TxConfig, 1)
				btx := blobfactory.RandBlobTxsWithNamespacesAndSigner(
					signer,
					[]share.Namespace{ns1},
					[]int{100},
				)[0]
				return btx
			},
			expectedABCICode: abci.CodeTypeOK,
		},
		{
			name:      "normal transaction, CheckTxType_Recheck",
			checkType: abci.CheckTxType_Recheck,
			getTx: func() []byte {
				signer := createSigner(t, kr, accs[1], encCfg.TxConfig, 2)
				btx := blobfactory.RandBlobTxsWithNamespacesAndSigner(
					signer,
					[]share.Namespace{ns1},
					[]int{100},
				)[0]
				return btx
			},
			expectedABCICode: abci.CodeTypeOK,
		},
		{
			name:      "invalid transaction, mismatched namespace",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := createSigner(t, kr, accs[2], encCfg.TxConfig, 3)
				btx := blobfactory.RandBlobTxsWithNamespacesAndSigner(
					signer,
					[]share.Namespace{ns1},
					[]int{100},
				)[0]

				dtx, _, err := tx.UnmarshalBlobTx(btx)
				require.NoError(t, err)
				newBlob, err := share.NewBlob(share.RandomBlobNamespace(), dtx.Blobs[0].Data(), appconsts.DefaultShareVersion, nil)
				require.NoError(t, err)
				dtx.Blobs[0] = newBlob
				bbtx, err := tx.MarshalBlobTx(dtx.Tx, dtx.Blobs[0])
				require.NoError(t, err)
				return bbtx
			},
			expectedABCICode: blobtypes.ErrNamespaceMismatch.ABCICode(),
			expectedLog:      blobtypes.ErrNamespaceMismatch.Error(),
		},
		{
			name:      "PFB with no blob, CheckTxType_New",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := createSigner(t, kr, accs[3], encCfg.TxConfig, 4)
				btx := blobfactory.RandBlobTxsWithNamespacesAndSigner(
					signer,
					[]share.Namespace{ns1},
					[]int{100},
				)[0]
				dtx, _ := coretypes.UnmarshalBlobTx(btx)
				return dtx.Tx
			},
			expectedABCICode: blobtypes.ErrNoBlobs.ABCICode(),
			expectedLog:      blobtypes.ErrNoBlobs.Error(),
		},
		{
			name:      "normal blobTx w/ multiple blobs, CheckTxType_New",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := createSigner(t, kr, accs[4], encCfg.TxConfig, 5)
				_, blobs := blobfactory.RandMsgPayForBlobsWithSigner(tmrand.NewRand(), signer.Account(accs[4]).Address().String(), 10_000, 10)
				tx, _, err := signer.CreatePayForBlobs(accs[4], blobs, opts...)
				require.NoError(t, err)
				return tx
			},
			expectedABCICode: abci.CodeTypeOK,
		},
		{
			name:      "1,000 byte blob",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := createSigner(t, kr, accs[5], encCfg.TxConfig, 6)
				_, blobs := blobfactory.RandMsgPayForBlobsWithSigner(tmrand.NewRand(), signer.Account(accs[5]).Address().String(), 1_000, 1)
				tx, _, err := signer.CreatePayForBlobs(accs[5], blobs, opts...)
				require.NoError(t, err)
				return tx
			},
			expectedABCICode: abci.CodeTypeOK,
		},
		{
			name:      "10,000 byte blob",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := createSigner(t, kr, accs[6], encCfg.TxConfig, 7)
				_, blobs := blobfactory.RandMsgPayForBlobsWithSigner(tmrand.NewRand(), signer.Account(accs[6]).Address().String(), 10_000, 1)
				tx, _, err := signer.CreatePayForBlobs(accs[6], blobs, opts...)
				require.NoError(t, err)
				return tx
			},
			expectedABCICode: abci.CodeTypeOK,
		},
		{
			name:      "100,000 byte blob",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := createSigner(t, kr, accs[7], encCfg.TxConfig, 8)
				_, blobs := blobfactory.RandMsgPayForBlobsWithSigner(tmrand.NewRand(), signer.Account(accs[7]).Address().String(), 100_000, 1)
				tx, _, err := signer.CreatePayForBlobs(accs[7], blobs, opts...)
				require.NoError(t, err)
				return tx
			},
			expectedABCICode: abci.CodeTypeOK,
		},
		{
			name:      "1,000,000 byte blob",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := createSigner(t, kr, accs[8], encCfg.TxConfig, 9)
				_, blobs := blobfactory.RandMsgPayForBlobsWithSigner(tmrand.NewRand(), signer.Account(accs[8]).Address().String(), 1_000_000, 1)
				tx, _, err := signer.CreatePayForBlobs(accs[8], blobs, opts...)
				require.NoError(t, err)
				return tx
			},
			expectedABCICode: abci.CodeTypeOK,
		},
		{
			name:      "2,000,000 byte blob",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := createSigner(t, kr, accs[9], encCfg.TxConfig, 10)
				_, blobs := blobfactory.RandMsgPayForBlobsWithSigner(tmrand.NewRand(), signer.Account(accs[9]).Address().String(), 2_000_000, 1)
				tx, _, err := signer.CreatePayForBlobs(accs[9], blobs, opts...)
				require.NoError(t, err)
				return tx
			},
			expectedABCICode: blobtypes.ErrBlobsTooLarge.ABCICode(),
			expectedLog:      blobtypes.ErrBlobsTooLarge.Error(),
		},
		{
			name:      "v1 blob with invalid signer",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := createSigner(t, kr, accs[10], encCfg.TxConfig, 11)
				blob, err := share.NewV1Blob(share.RandomBlobNamespace(), []byte("data"), signer.Account(accs[10]).Address())
				require.NoError(t, err)
				blobTx, _, err := signer.CreatePayForBlobs(accs[10], []*share.Blob{blob}, opts...)
				require.NoError(t, err)
				blob, err = share.NewV1Blob(share.RandomBlobNamespace(), []byte("data"), testnode.RandomAddress().(sdk.AccAddress))
				require.NoError(t, err)
				bTx, _, err := tx.UnmarshalBlobTx(blobTx)
				require.NoError(t, err)
				bTx.Blobs[0] = blob
				blobTxBytes, err := tx.MarshalBlobTx(bTx.Tx, bTx.Blobs[0])
				require.NoError(t, err)
				return blobTxBytes
			},
			expectedABCICode: blobtypes.ErrInvalidBlobSigner.ABCICode(),
			expectedLog:      blobtypes.ErrInvalidBlobSigner.Error(),
		},
		{
			name:      "v1 blob with valid signer",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := createSigner(t, kr, accs[10], encCfg.TxConfig, 11)
				blob, err := share.NewV1Blob(share.RandomBlobNamespace(), []byte("data"), signer.Account(accs[10]).Address())
				require.NoError(t, err)
				blobTx, _, err := signer.CreatePayForBlobs(accs[10], []*share.Blob{blob}, opts...)
				require.NoError(t, err)
				return blobTx
			},
			expectedABCICode: abci.CodeTypeOK,
		},
		{
			name:      "v1 blob over 2MiB",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := createSigner(t, kr, accs[11], encCfg.TxConfig, 12)
				blob, err := share.NewV1Blob(share.RandomBlobNamespace(), bytes.Repeat([]byte{1}, 2097152), signer.Account(accs[11]).Address())
				require.NoError(t, err)
				blobTx, _, err := signer.CreatePayForBlobs(accs[11], []*share.Blob{blob}, opts...)
				require.NoError(t, err)
				return blobTx
			},
			expectedLog:      apperr.ErrTxExceedsMaxSize.Error(),
			expectedABCICode: apperr.ErrTxExceedsMaxSize.ABCICode(),
		},
		{
			name:      "v0 blob over 2MiB",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := createSigner(t, kr, accs[12], encCfg.TxConfig, 13)
				blob, err := share.NewV0Blob(share.RandomBlobNamespace(), bytes.Repeat([]byte{1}, 2097152))
				require.NoError(t, err)
				blobTx, _, err := signer.CreatePayForBlobs(accs[12], []*share.Blob{blob}, opts...)
				require.NoError(t, err)
				return blobTx
			},
			expectedLog:      apperr.ErrTxExceedsMaxSize.Error(),
			expectedABCICode: apperr.ErrTxExceedsMaxSize.ABCICode(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := testApp.CheckTx(abci.RequestCheckTx{Type: tt.checkType, Tx: tt.getTx()})
			assert.Equal(t, tt.expectedABCICode, resp.Code, resp.Log)
			assert.Contains(t, resp.Log, tt.expectedLog)
		})
	}
}

func createSigner(t *testing.T, kr keyring.Keyring, accountName string, enc client.TxConfig, accNum uint64) *user.Signer {
	signer, err := user.NewSigner(kr, enc, testutil.ChainID, appconsts.LatestVersion, user.NewAccount(accountName, accNum, 0))
	require.NoError(t, err)
	return signer
}
