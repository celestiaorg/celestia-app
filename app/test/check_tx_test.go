package app_test

import (
	"bytes"
	"testing"

	tmrand "cosmossdk.io/math/unsafe"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	apperr "github.com/celestiaorg/celestia-app/v4/app/errors"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/v4/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/go-square/v2/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Here we only need to check the functionality that is added to CheckTx. We
// assume that the rest of CheckTx is tested by the cosmos-sdk.
func TestCheckTx(t *testing.T) {

	var (
		err  error
		resp *abci.ResponseCheckTx
		ns1  share.Namespace
	)

	enc := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	ns1, err = share.NewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	require.NoError(t, err)

	accs := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"}
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accs...)

	signers := make([]*user.Signer, len(accs))
	for i, acc := range accs {
		fetchedAcc := testutil.DirectQueryAccount(testApp, testfactory.GetAddress(kr, acc))
		signers[i] = createSigner(t, kr, acc, enc.TxConfig, fetchedAcc.GetAccountNumber())
	}

	opts := blobfactory.FeeTxOpts(1e9)
	type test struct {
		name             string
		checkType        abci.CheckTxType
		getTx            func() []byte
		expectedABCICode uint32
	}

	tests := []test{
		{
			name:      "normal transaction, CheckTxType_New",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				btx := blobfactory.RandBlobTxsWithNamespacesAndSigner(
					signers[0],
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
				btx := blobfactory.RandBlobTxsWithNamespacesAndSigner(
					signers[1],
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
				btx := blobfactory.RandBlobTxsWithNamespacesAndSigner(
					signers[2],
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
		},
		{
			name:      "PFB with no blob, CheckTxType_New",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				btx := blobfactory.RandBlobTxsWithNamespacesAndSigner(
					signers[3],
					[]share.Namespace{ns1},
					[]int{100},
				)[0]
				dtx, _ := coretypes.UnmarshalBlobTx(btx)
				return dtx.Tx
			},
			expectedABCICode: blobtypes.ErrNoBlobs.ABCICode(),
		},
		{
			name:      "normal blobTx w/ multiple blobs, CheckTxType_New",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := signers[4]
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
				signer := signers[5]
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
				signer := signers[6]
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
				signer := signers[7]
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
				signer := signers[8]
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
				signer := signers[9]
				_, blobs := blobfactory.RandMsgPayForBlobsWithSigner(tmrand.NewRand(), signer.Account(accs[9]).Address().String(), 2_000_000, 1)
				tx, _, err := signer.CreatePayForBlobs(accs[9], blobs, opts...)
				require.NoError(t, err)
				return tx
			},
			expectedABCICode: blobtypes.ErrTotalBlobSizeTooLarge.ABCICode(),
		},
		{
			name:      "v1 blob with invalid signer",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := signers[10]
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
		},
		{
			name:      "v1 blob with valid signer",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := signers[10]
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
				signer := signers[11]
				blob, err := share.NewV1Blob(share.RandomBlobNamespace(), bytes.Repeat([]byte{1}, 2097152), signer.Account(accs[11]).Address())
				require.NoError(t, err)
				blobTx, _, err := signer.CreatePayForBlobs(accs[11], []*share.Blob{blob}, opts...)
				require.NoError(t, err)
				return blobTx
			},
			expectedABCICode: apperr.ErrTxExceedsMaxSize.ABCICode(),
		},
		{
			name:      "v0 blob over 2MiB",
			checkType: abci.CheckTxType_New,
			getTx: func() []byte {
				signer := signers[12]
				blob, err := share.NewV0Blob(share.RandomBlobNamespace(), bytes.Repeat([]byte{1}, 2097152))
				require.NoError(t, err)
				blobTx, _, err := signer.CreatePayForBlobs(accs[12], []*share.Blob{blob}, opts...)
				require.NoError(t, err)
				return blobTx
			},
			expectedABCICode: apperr.ErrTxExceedsMaxSize.ABCICode(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err = testApp.CheckTx(&abci.RequestCheckTx{Type: tt.checkType, Tx: tt.getTx()})
			if resp.Code == 0 {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.expectedABCICode, resp.Code, resp.Log)
		})
	}
}

func createSigner(t *testing.T, kr keyring.Keyring, accountName string, enc client.TxConfig, accNum uint64) *user.Signer {
	t.Helper()

	signer, err := user.NewSigner(kr, enc, testutil.ChainID, appconsts.LatestVersion, user.NewAccount(accountName, accNum, 0))
	require.NoError(t, err)
	return signer
}
