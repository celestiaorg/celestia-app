package v2

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app/params"
	"github.com/celestiaorg/celestia-app/v6/pkg/user/utils"
	"github.com/celestiaorg/celestia-app/v6/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v6/test/util/random"
	"github.com/celestiaorg/celestia-app/v6/test/util/testnode"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestV2SubmitMethods(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Setup test client
	_, txClient, ctx := utils.SetupTxClientWithDefaultParams(t)
	v2Client := NewTxClient(txClient)
	serviceClient := sdktx.NewServiceClient(ctx.GRPCClient)
	testCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	blobs := blobfactory.ManyRandBlobs(random.New(), 1e3, 1e4)
	addr := txClient.DefaultAddress()
	msg := bank.NewMsgSend(addr, testnode.RandomAddress().(sdktypes.AccAddress), sdktypes.NewCoins(sdktypes.NewInt64Coin(params.BondDenom, 10)))
	expectedSigner := txClient.DefaultAddress().String()

	testCases := []struct {
		name           string
		submitFunc     func() (*sdktypes.TxResponse, error)
		expectedSigner string
	}{
		{
			name: "SubmitPayForBlob",
			submitFunc: func() (*sdktypes.TxResponse, error) {
				return v2Client.SubmitPayForBlob(testCtx, blobs)
			},
			expectedSigner: expectedSigner,
		},
		{
			name: "SubmitPayForBlobWithAccount",
			submitFunc: func() (*sdktypes.TxResponse, error) {
				return v2Client.SubmitPayForBlobWithAccount(testCtx, txClient.DefaultAccountName(), blobs)
			},
			expectedSigner: expectedSigner,
		},
		{
			name: "SubmitPayForBlobToQueue",
			submitFunc: func() (*sdktypes.TxResponse, error) {
				return v2Client.SubmitPayForBlobToQueue(testCtx, blobs)
			},
			expectedSigner: expectedSigner,
		},
		{
			name: "SubmitTx",
			submitFunc: func() (*sdktypes.TxResponse, error) {
				return v2Client.SubmitTx(testCtx, []sdktypes.Msg{msg})
			},
			expectedSigner: expectedSigner,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			submitResp, err := tc.submitFunc()
			require.NoError(t, err)
			utils.VerifyTxResponse(t, context.Background(), serviceClient, submitResp)

			// verify signers
			require.Equal(t, len(submitResp.Signers), 1)
			require.Equal(t, submitResp.Signers[0], txClient.DefaultAddress().String())
		})
	}
}
