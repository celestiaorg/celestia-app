package app_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	"cosmossdk.io/x/feegrant"
	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v10/test/util/testnode"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4"
	abci "github.com/cometbft/cometbft/abci/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

// gasPerAdditionalSDKMessage mirrors the unexported constant in app.
const gasPerAdditionalSDKMessage = 5_000

// TestSDKMessageGasAdmission checks that a tx whose gas limit does not cover
// gasPerAdditionalSDKMessage per counted SDK message beyond the first is kept out of the mempool
// and out of proposals this node builds, while blocks that include one stay
// valid.
func TestSDKMessageGasAdmission(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(7)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	seedICAHostChannel(t, testApp)
	infos := queryAccountInfo(testApp, accounts, kr)

	signer := func(i int) (*user.Signer, sdk.AccAddress) {
		s, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(accounts[i], infos[i].AccountNum, infos[i].Sequence))
		require.NoError(t, err)
		return s, testfactory.GetAddress(kr, accounts[i])
	}
	createTx := func(s *user.Signer, gasLimit uint64, msgs ...sdk.Msg) []byte {
		tx, _, err := s.CreateTx(msgs, user.SetGasLimitAndGasPrice(gasLimit, appconsts.DefaultMinGasPrice))
		require.NoError(t, err)
		return tx
	}
	send := func(from sdk.AccAddress) sdk.Msg {
		return banktypes.NewMsgSend(from, testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10)))
	}
	// Six MsgExec with 99 empty MsgSends each weigh MaxSDKMessages.
	emptyExecs := func(addr sdk.AccAddress) []sdk.Msg {
		msgs := make([]sdk.Msg, 6)
		for i := range msgs {
			inner := make([]sdk.Msg, 99)
			for j := range inner {
				inner[j] = &banktypes.MsgSend{}
			}
			exec := authz.NewMsgExec(addr, inner)
			msgs[i] = &exec
		}
		return msgs
	}
	// An ICA packet with MaxSDKMessages-1 junk entries weighs MaxSDKMessages.
	junkICAPacket := func(addr sdk.AccAddress) sdk.Msg {
		entries := make([]*codectypes.Any, appconsts.MaxSDKMessages-1)
		for i := range entries {
			entries[i] = &codectypes.Any{TypeUrl: "x"}
		}
		payload, err := enc.Codec.Marshal(&icatypes.CosmosTx{Messages: entries})
		require.NoError(t, err)
		data := icatypes.InterchainAccountPacketData{Type: icatypes.EXECUTE_TX, Data: payload}
		packet := channeltypes.NewPacket(data.GetBytes(), 1, "icacontroller-test", "channel-0", icatypes.HostPortID, icaChannelID, clienttypes.NewHeight(1, 1000000), 0)
		return channeltypes.NewMsgRecvPacket(packet, []byte("proof"), clienttypes.NewHeight(1, 1), addr.String())
	}

	// Enough gas to execute each attack, but less than the per-message floor.
	const underpaidGas = 1_000_000
	floor := uint64(appconsts.MaxSDKMessages-1) * gasPerAdditionalSDKMessage

	s0, addr0 := signer(0)
	execAttack := createTx(s0, underpaidGas, emptyExecs(addr0)...)
	s1, addr1 := signer(1)
	icaAttack := createTx(s1, underpaidGas, junkICAPacket(addr1))
	s2, addr2 := signer(2)
	paidExec := createTx(s2, floor, emptyExecs(addr2)...)

	// Honest txs with many messages, each with a gas limit close to what it uses.
	s6, addr6 := signer(6)
	honestSends := make([]sdk.Msg, 20)
	for i := range honestSends {
		honestSends[i] = send(addr6)
	}
	honestExec := authz.NewMsgExec(addr6, honestSends)
	honestSendsTx := createTx(s6, 2_000_000, honestSends...)
	require.NoError(t, s6.IncrementSequence(accounts[6]))
	honestTxs := [][]byte{honestSendsTx, createTx(s6, 2_000_000, &honestExec)}

	sendTxs := make([][]byte, 3)
	for i := range sendTxs {
		s, addr := signer(3 + i)
		sendTxs[i] = createTx(s, 100_000, send(addr))
	}

	t.Run("CheckTx rejects an underpaid tx", func(t *testing.T) {
		for _, tx := range [][]byte{execAttack, icaAttack} {
			for _, typ := range []abci.CheckTxType{abci.CheckTxType_New, abci.CheckTxType_Recheck} {
				resp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: typ})
				require.NoError(t, err)
				require.Equal(t, sdkerrors.ErrOutOfGas.ABCICode(), resp.Code, resp.Log)
				require.Equal(t, sdkerrors.ErrOutOfGas.Codespace(), resp.Codespace)
			}
		}
	})

	t.Run("CheckTx accepts a tx that pays the floor", func(t *testing.T) {
		resp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: paidExec, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code, resp.Log)
	})

	t.Run("PrepareProposal keeps honest txs", func(t *testing.T) {
		resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
			Txs:    append([][]byte{execAttack, icaAttack}, sendTxs...),
			Height: testApp.LastBlockHeight() + 1,
			Time:   time.Now(),
		})
		require.NoError(t, err)
		require.Equal(t, sendTxs, resp.Txs)
	})

	t.Run("ProcessProposal accepts an underpaid tx", func(t *testing.T) {
		for _, tx := range [][]byte{execAttack, icaAttack} {
			blockTxs := [][]byte{tx}
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
		}
	})

	t.Run("honest txs use more gas than the floor", func(t *testing.T) {
		resp := commitBlock(t, testApp, honestTxs...)
		require.Len(t, resp.TxResults, len(honestTxs))
		weights := []int64{int64(len(honestSends)), int64(1 + len(honestSends))}
		for i, res := range resp.TxResults {
			require.Equal(t, abci.CodeTypeOK, res.Code, res.Log)
			require.GreaterOrEqual(t, res.GasUsed, (weights[i]-1)*gasPerAdditionalSDKMessage)
		}
	})
}

// TestSDKMessageGasFloorBelowCheapHonestMessages checks that the cheapest honest
// messages simulate to at least the floor, so a tx whose gas limit is its
// simulated gas, with no multiplier, passes CheckTx.
func TestSDKMessageGasFloorBelowCheapHonestMessages(t *testing.T) {
	const n = 50
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	// 0 signs direct messages, 1 grants authz to 2, which signs MsgExec.
	accounts := testfactory.GenerateAccounts(3)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	signers := make([]*user.Signer, len(accounts))
	addrs := make([]sdk.AccAddress, len(accounts))
	for i := range accounts {
		s, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(accounts[i], infos[i].AccountNum, infos[i].Sequence))
		require.NoError(t, err)
		signers[i], addrs[i] = s, testfactory.GetAddress(kr, accounts[i])
	}
	createTx := func(i int, gasLimit uint64, msgs ...sdk.Msg) []byte {
		tx, _, err := signers[i].CreateTx(msgs, user.SetGasLimitAndGasPrice(gasLimit, appconsts.DefaultMinGasPrice))
		require.NoError(t, err)
		return tx
	}
	commit := func(i int, msgs ...sdk.Msg) {
		resp := commitBlock(t, testApp, createTx(i, 50_000_000, msgs...))
		require.Equal(t, abci.CodeTypeOK, resp.TxResults[0].Code, resp.TxResults[0].Log)
		require.NoError(t, signers[i].IncrementSequence(accounts[i]))
	}

	// Setup: n proposals in voting period, n fee grants from each of 0 and 1,
	// and authz grants from 1 to 2.
	props := make([]sdk.Msg, n)
	deposit := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(10_000_000_000)))
	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName)
	for i := range props {
		content := banktypes.NewMsgSend(govAddr, addrs[0], sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
		p, err := govv1.NewMsgSubmitProposal([]sdk.Msg{content}, deposit, addrs[0].String(), "", fmt.Sprintf("title %d", i), "summary", false)
		require.NoError(t, err)
		props[i] = p
	}
	commit(0, props...)
	feeGrantees := make([][]sdk.AccAddress, 2)
	for granter := range feeGrantees {
		grants := make([]sdk.Msg, n)
		for i := range grants {
			grantee := testnode.RandomAddress().(sdk.AccAddress)
			feeGrantees[granter] = append(feeGrantees[granter], grantee)
			m, err := feegrant.NewMsgGrantAllowance(&feegrant.BasicAllowance{}, addrs[granter], grantee)
			require.NoError(t, err)
			grants[i] = m
		}
		commit(granter, grants...)
	}
	authzGrants := make([]sdk.Msg, 0, 3)
	for _, msg := range []sdk.Msg{&feegrant.MsgRevokeAllowance{}, &govv1.MsgVote{}, &distrtypes.MsgSetWithdrawAddress{}} {
		g, err := authz.NewMsgGrant(addrs[1], addrs[2], authz.NewGenericAuthorization(sdk.MsgTypeURL(msg)), nil)
		require.NoError(t, err)
		authzGrants = append(authzGrants, g)
	}
	commit(1, authzGrants...)

	shapes := []struct {
		name string
		msgs func(owner int) []sdk.Msg
	}{
		{"MsgRevokeAllowance", func(owner int) []sdk.Msg {
			out := make([]sdk.Msg, n)
			for i := range out {
				m := feegrant.NewMsgRevokeAllowance(addrs[owner], feeGrantees[owner][i])
				out[i] = &m
			}
			return out
		}},
		{"MsgVote", func(owner int) []sdk.Msg {
			out := make([]sdk.Msg, n)
			for i := range out {
				out[i] = govv1.NewMsgVote(addrs[owner], uint64(i+1), govv1.OptionYes, "")
			}
			return out
		}},
		{"MsgSetWithdrawAddress", func(owner int) []sdk.Msg {
			out := make([]sdk.Msg, n)
			for i := range out {
				out[i] = distrtypes.NewMsgSetWithdrawAddress(addrs[owner], testnode.RandomAddress().(sdk.AccAddress))
			}
			return out
		}},
	}
	for _, shape := range shapes {
		exec := authz.NewMsgExec(addrs[2], shape.msgs(1))
		cases := []struct {
			name   string
			signer int
			weight int
			msgs   []sdk.Msg
		}{
			{shape.name, 0, n, shape.msgs(0)},
			{"MsgExec{" + shape.name + "}", 2, n + 1, []sdk.Msg{&exec}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				gasInfo, _, err := testApp.Simulate(createTx(tc.signer, 50_000_000, tc.msgs...))
				require.NoError(t, err)
				floor := uint64(tc.weight-1) * gasPerAdditionalSDKMessage
				require.GreaterOrEqual(t, gasInfo.GasUsed, floor)

				resp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: createTx(tc.signer, gasInfo.GasUsed, tc.msgs...), Type: abci.CheckTxType_New})
				require.NoError(t, err)
				require.Equal(t, abci.CodeTypeOK, resp.Code, resp.Log)
				require.NoError(t, signers[tc.signer].IncrementSequence(accounts[tc.signer]))
			})
		}
	}
}
