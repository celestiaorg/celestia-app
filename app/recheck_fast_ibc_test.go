package app_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v12/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v12/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v12/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/crypto/tmhash"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmtprotoversion "github.com/cometbft/cometbft/proto/tendermint/version"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cometbft/cometbft/version"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	commitmenttypes "github.com/cosmos/ibc-go/v8/modules/core/23-commitment/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
	ibctm "github.com/cosmos/ibc-go/v8/modules/light-clients/07-tendermint"
	"github.com/stretchr/testify/require"
)

// The IBC client tests run against a one-validator counterparty chain whose
// client is trusted at height 4, with a consensus state timestamped at genesis.
const ibcChainID = "counterparty"

var ibcPV = cmttypes.NewMockPVWithParams(ed25519.GenPrivKeyFromSecret([]byte(ibcChainID)), false, false)

func ibcVals() *cmttypes.ValidatorSet {
	pk, _ := ibcPV.GetPubKey()
	return cmttypes.NewValidatorSet([]*cmttypes.Validator{cmttypes.NewValidator(pk, 1000)})
}

// commitWithFees commits a block of one outside tx, so the fee collector holds
// fees and pooled txs can replay.
func (e *eqEnv) commitWithFees() {
	last := len(e.names) - 1
	e.commit([][]byte{e.sendTx(last, e.committedSeq(last), 0, math.OneInt(), user.SetGasLimit(200_000), user.SetFee(2_000))})
}

// createIBCClient creates the counterparty's client in a block on both apps,
// paid by sender i. The client expires trusting after genesis.
func (e *eqEnv) createIBCClient(i int, trusting time.Duration) string {
	cs := &ibctm.ClientState{
		ChainId: ibcChainID, TrustLevel: ibctm.DefaultTrustLevel, TrustingPeriod: trusting, UnbondingPeriod: 2 * trusting,
		MaxClockDrift: time.Hour, LatestHeight: clienttypes.NewHeight(0, 4), ProofSpecs: commitmenttypes.GetSDKSpecs(),
	}
	cons := &ibctm.ConsensusState{
		Timestamp: testutil.GenesisTime, Root: commitmenttypes.NewMerkleRoot([]byte("root")), NextValidatorsHash: ibcVals().Hash(),
	}
	msg, err := clienttypes.NewMsgCreateClient(cs, cons, e.addrs[i].String())
	require.NoError(e.t, err)
	id := clienttypes.FormatClientIdentifier(ibcexported.Tendermint, e.full.IBCKeeper.ClientKeeper.GetNextClientSequence(committedCtx(e.full)))
	require.NoError(e.t, e.signer.SetSequence(e.names[i], e.committedSeq(i)))
	raw, _, err := e.signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(1_000_000), user.SetFee(10_000))
	require.NoError(e.t, err)
	e.commit([][]byte{raw}) // requires the same app hash on both apps
	_, found := e.full.IBCKeeper.ClientKeeper.GetClientState(committedCtx(e.full), id)
	require.True(e.t, found, "client %s not created", id)
	return id
}

// updateClientMsg updates client id to a counterparty header at height (> 4),
// signed by sender i.
func (e *eqEnv) updateClientMsg(i int, id string, height int64) sdk.Msg {
	vals := ibcVals()
	h := cmttypes.Header{
		Version: cmtprotoversion.Consensus{Block: version.BlockProtocol, App: 1}, ChainID: ibcChainID, Height: height,
		Time:           testutil.GenesisTime.Add(time.Duration(height) * time.Second),
		ValidatorsHash: vals.Hash(), NextValidatorsHash: vals.Hash(), ProposerAddress: vals.Proposer.Address,
	}
	blockID := cmttypes.BlockID{Hash: h.Hash(), PartSetHeader: cmttypes.PartSetHeader{Total: 1, Hash: tmhash.Sum([]byte("parts"))}}
	vote, err := cmttypes.MakeVote(ibcPV, ibcChainID, 0, height, 0, cmtproto.PrecommitType, blockID, h.Time)
	require.NoError(e.t, err)
	sh := cmttypes.SignedHeader{Header: &h, Commit: &cmttypes.Commit{Height: height, BlockID: blockID, Signatures: []cmttypes.CommitSig{vote.CommitSig()}}}
	vp, err := vals.ToProto()
	require.NoError(e.t, err)
	header := &ibctm.Header{SignedHeader: sh.ToProto(), ValidatorSet: vp, TrustedHeight: clienttypes.NewHeight(0, 4), TrustedValidators: vp}
	msg, err := clienttypes.NewMsgUpdateClient(id, header, e.addrs[i].String())
	require.NoError(e.t, err)
	return msg
}

// updateClientTx builds sender i's tx updating client id to height, followed by
// extra. The relay ante stops at the first message that is not IBC's, so the
// update must come first to be checked.
func (e *eqEnv) updateClientTx(i int, seq uint64, id string, height int64, extra ...sdk.Msg) []byte {
	require.NoError(e.t, e.signer.SetSequence(e.names[i], seq))
	raw, _, err := e.signer.CreateTx(append([]sdk.Msg{e.updateClientMsg(i, id, height)}, extra...), user.SetGasLimit(1_000_000), user.SetFee(10_000))
	require.NoError(e.t, err)
	return raw
}

// TestFastRecheckIBCUpdateClient checks that a pooled MsgUpdateClient, whose
// relay ante writes the block time and height and expires with block time,
// rechecks as in full on the fast app.
func TestFastRecheckIBCUpdateClient(t *testing.T) {
	const trusting = 2 * time.Hour
	for _, withSend := range []bool{false, true} {
		t.Run(fmt.Sprintf("send=%v", withSend), func(t *testing.T) {
			e := newEqEnv(t, 51, 8)
			defer e.full.Close()
			defer e.fast.Close()
			const payer = 1
			id := e.createIBCClient(2, trusting)
			var extra []sdk.Msg
			if withSend {
				extra = append(extra, banktypes.NewMsgSend(e.addrs[payer], e.addrs[0], sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.OneInt()))))
			}
			seq := e.checkSeq(payer)
			raw := e.updateClientTx(payer, seq, id, 5, extra...)
			rA := e.checkTx(e.full, raw, abci.CheckTxType_New)
			rB := e.checkTx(e.fast, raw, abci.CheckTxType_New)
			require.Equal(t, abci.CodeTypeOK, rA.Code, rA.Log)
			require.Equal(t, rA.Code, rB.Code)
			require.Equal(t, rA.GasUsed, rB.GasUsed)
			ptx := eqPoolTx{raw: raw, seq: seq, kind: "ibc"}
			e.pool[payer] = append(e.pool[payer], ptx)

			// A block without it: the block time and height move on.
			e.commitWithFees()
			e.recheck()
			e.requireSameCheckState(0)
			require.Contains(t, e.pool[payer], ptx)

			// Past the trusting period the client expires and the full recheck rejects.
			e.blockTime = testutil.GenesisTime.Add(trusting)
			e.commitWithFees()
			e.recheck()
			e.requireSameCheckState(1)
			require.NotContains(t, e.pool[payer], ptx)
		})
	}
}
