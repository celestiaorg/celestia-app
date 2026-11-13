package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testnode"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	"github.com/stretchr/testify/require"
)

// TestModuleQuerySafeRequestsAdmission checks that a MsgModuleQuerySafe with
// more than one request is kept out of the mempool and out of proposals this
// node builds, while blocks that include one stay valid.
func TestModuleQuerySafeRequestsAdmission(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(4)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)

	signer := func(i int) (*user.Signer, sdk.AccAddress) {
		s, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(accounts[i], infos[i].AccountNum, infos[i].Sequence))
		require.NoError(t, err)
		return s, testfactory.GetAddress(kr, accounts[i])
	}
	querySafe := func(addr sdk.AccAddress, numRequests int) *icahosttypes.MsgModuleQuerySafe {
		requests := make([]*icahosttypes.QueryRequest, numRequests)
		for i := range requests {
			requests[i] = &icahosttypes.QueryRequest{Path: "/cosmos.bank.v1beta1.Query/TotalSupply"}
		}
		return icahosttypes.NewMsgModuleQuerySafe(addr.String(), requests)
	}
	createTx := func(s *user.Signer, msgs ...sdk.Msg) []byte {
		tx, _, err := s.CreateTx(msgs, user.SetGasLimitAndGasPrice(1e7, appconsts.DefaultMinGasPrice))
		require.NoError(t, err)
		return tx
	}

	s0, addr0 := signer(0)
	heavyTx := createTx(s0, querySafe(addr0, 2))

	s1, addr1 := signer(1)
	exec := authz.NewMsgExec(addr1, []sdk.Msg{querySafe(addr1, 2)})
	heavyExecTx := createTx(s1, &exec)

	s2, addr2 := signer(2)
	lightTx := createTx(s2, querySafe(addr2, 1))

	s3, addr3 := signer(3)
	sendTx := createTx(s3, banktypes.NewMsgSend(addr3, testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10))))

	t.Run("CheckTx rejects more than one request", func(t *testing.T) {
		for _, tx := range [][]byte{heavyTx, heavyExecTx} {
			for _, typ := range []abci.CheckTxType{abci.CheckTxType_New, abci.CheckTxType_Recheck} {
				resp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: typ})
				require.NoError(t, err)
				require.Equal(t, sdkerrors.ErrInvalidRequest.ABCICode(), resp.Code, resp.Log)
				require.Equal(t, sdkerrors.ErrInvalidRequest.Codespace(), resp.Codespace)
			}
		}
	})

	t.Run("CheckTx accepts one request", func(t *testing.T) {
		resp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: lightTx, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code, resp.Log)
	})

	t.Run("PrepareProposal drops more than one request", func(t *testing.T) {
		resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
			Txs:    [][]byte{heavyTx, heavyExecTx, lightTx, sendTx},
			Height: testApp.LastBlockHeight() + 1,
			Time:   time.Now(),
		})
		require.NoError(t, err)
		require.Equal(t, [][]byte{lightTx, sendTx}, resp.Txs)
	})

	blockTxs := [][]byte{heavyTx, heavyExecTx, sendTx}

	t.Run("ProcessProposal accepts more than one request", func(t *testing.T) {
		classifiedTxs, err := fibretypes.ClassifyTxs(blockTxs)
		require.NoError(t, err)
		dataSquare, err := square.Construct(classifiedTxs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
		require.NoError(t, err)
		squareSize, err := dataSquare.Size()
		require.NoError(t, err)

		resp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
			Height:       testApp.LastBlockHeight() + 1,
			Time:         time.Now(),
			Txs:          blockTxs,
			DataRootHash: calculateNewDataHash(t, blockTxs),
			SquareSize:   uint64(squareSize),
		})
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status)
	})

	t.Run("FinalizeBlock executes more than one request", func(t *testing.T) {
		resp := commitBlock(t, testApp, blockTxs...)
		require.Len(t, resp.TxResults, len(blockTxs))
		for i, res := range resp.TxResults {
			t.Logf("tx %d: code=%d codespace=%q gas_used=%d log=%q", i, res.Code, res.Codespace, res.GasUsed, res.Log)
			require.Equal(t, abci.CodeTypeOK, res.Code, res.Log)
		}
	})
}
