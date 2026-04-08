//go:build fibre

package da

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	fibretypes "github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	squarev4 "github.com/celestiaorg/go-square/v4"
	sh "github.com/celestiaorg/go-square/v4/share"
	gotx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/cosmos/btcutil/bech32"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cosmostx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
)

func TestConstructEDS_WithFibreTx(t *testing.T) {
	fibreTx := buildMsgPayForFibreTxBytes(t)

	type testCase struct {
		name string
		txs  [][]byte
	}

	testCases := []testCase{
		{
			name: "fibre tx only",
			txs:  [][]byte{fibreTx},
		},
		{
			name: "normal tx and fibre tx",
			// squarev4.Construct requires ordering: normal txs, then blob txs, then fibre txs
			txs: [][]byte{bytes.Repeat([]byte{0x01}, 200), fibreTx},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Verify that the data square contains PayForFibre namespace shares.
			square, err := squarev4.Construct(tc.txs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
			require.NoError(t, err)
			pffRange := sh.GetShareRangeForNamespace(square, sh.PayForFibreNamespace)
			require.False(t, pffRange.IsEmpty(), "expected PayForFibreNamespace shares in square")

			t.Run("without pool", func(t *testing.T) {
				eds, err := ConstructEDS(tc.txs, appconsts.Version, -1)
				require.NoError(t, err)
				require.NotNil(t, eds)
			})

			t.Run("with pool", func(t *testing.T) {
				eds, err := constructEDSWithPool(tc.txs, appconsts.Version, -1)
				require.NoError(t, err)
				require.NotNil(t, eds)
			})
		})
	}
}

// buildMsgPayForFibreTxBytes constructs Cosmos SDK Tx proto bytes containing a
// single MsgPayForFibre message.
func buildMsgPayForFibreTxBytes(t *testing.T) []byte {
	t.Helper()
	ns := sh.MustNewV0Namespace(bytes.Repeat([]byte{1}, sh.NamespaceVersionZeroIDSize))
	signerRaw := bytes.Repeat([]byte{0xAA}, sh.SignerSize)
	signer, err := bech32.EncodeFromBase256("celestia", signerRaw)
	require.NoError(t, err)
	commitment := bytes.Repeat([]byte{0xFF}, sh.FibreCommitmentSize)

	msg := &fibretypes.MsgPayForFibre{
		Signer: signer,
		PaymentPromise: fibretypes.PaymentPromise{
			Namespace:   ns.Bytes(),
			BlobVersion: fibretypes.BlobVersionZero,
			Commitment:  commitment,
		},
	}

	anyMsg, err := codectypes.NewAnyWithValue(msg)
	require.NoError(t, err)
	require.Equal(t, gotx.MsgPayForFibreTypeURL, anyMsg.TypeUrl,
		"cosmos-sdk TypeURL must match the constant that TryParseFibreTx checks")

	body := &cosmostx.TxBody{
		Messages: []*codectypes.Any{anyMsg},
	}
	tx := &cosmostx.Tx{Body: body}
	txBytes, err := tx.Marshal()
	require.NoError(t, err)
	return txBytes
}
