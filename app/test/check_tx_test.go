package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/testutil"
	"github.com/celestiaorg/celestia-app/testutil/blobfactory"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// Here we only need to check the functionality that is added to CheckTx. We
// assume that the rest of CheckTx is tested by the cosmos-sdk.
func TestCheckTx(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	accs := []string{"a", "b", "c", "d"}

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
					types.NewKeyringSigner(kr, accs[0], testutil.ChainID),
					[][]byte{
						{1, 1, 1, 1, 1, 1, 1, 1},
					},
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
					types.NewKeyringSigner(kr, accs[1], testutil.ChainID),
					[][]byte{
						{1, 1, 1, 1, 1, 1, 1, 1},
					},
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
					types.NewKeyringSigner(kr, accs[2], testutil.ChainID),
					[][]byte{
						{1, 1, 1, 1, 1, 1, 1, 1},
					},
					[]int{100},
				)[0]

				dtx, _ := coretypes.UnmarshalBlobTx(btx)
				dtx.Blobs[0].NamespaceId = appconsts.TxNamespaceID
				bbtx, err := coretypes.MarshalBlobTx(dtx.Tx, dtx.Blobs[0])
				require.NoError(t, err)
				return bbtx
			},
			expectedABCICode: types.ErrNamespaceMismatch.ABCICode(),
		},
	}

	for _, tt := range tests {
		resp := testApp.CheckTx(abci.RequestCheckTx{Type: tt.checkType, Tx: tt.getTx()})
		assert.Equal(t, tt.expectedABCICode, resp.Code, tt.name, resp.Log)
	}
}
