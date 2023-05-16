package app_test

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// Here we only need to check the functionality that is added to CheckTx. We
// assume that the rest of CheckTx is tested by the cosmos-sdk.
func TestCheckTx(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))

	accs := []string{"a", "b", "c", "d", "e", "f"}

	testApp, kr := testutil.SetupTestAppWithGenesisValSet(accs...)

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
					encCfg.TxConfig.TxEncoder(),
					blobtypes.NewKeyringSigner(kr, accs[0], testutil.ChainID),
					[]appns.Namespace{ns1},
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
					encCfg.TxConfig.TxEncoder(),
					blobtypes.NewKeyringSigner(kr, accs[1], testutil.ChainID),
					[]appns.Namespace{ns1},
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
					encCfg.TxConfig.TxEncoder(),
					blobtypes.NewKeyringSigner(kr, accs[2], testutil.ChainID),
					[]appns.Namespace{ns1},
					[]int{100},
				)[0]

				dtx, _ := coretypes.UnmarshalBlobTx(btx)
				dtx.Blobs[0].NamespaceId = appns.RandomBlobNamespace().ID
				bbtx, err := coretypes.MarshalBlobTx(dtx.Tx, dtx.Blobs[0])
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
					encCfg.TxConfig.TxEncoder(),
					blobtypes.NewKeyringSigner(kr, accs[3], testutil.ChainID),
					[]appns.Namespace{ns1},
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
				tx := blobfactory.RandBlobTxsWithAccounts(encCfg.TxConfig.TxEncoder(), kr, nil, 10000, 10, true, testutil.ChainID, accs[3:4])[0]
				return tx
			},
			expectedABCICode: abci.CodeTypeOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := testApp.CheckTx(abci.RequestCheckTx{Type: tt.checkType, Tx: tt.getTx()})
			assert.Equal(t, tt.expectedABCICode, resp.Code, tt.name, resp.Log)
		})
	}
}
