package qgb_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/x/qgb/types"

	"github.com/cosmos/cosmos-sdk/x/staking/teststaking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/x/qgb"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirstAttestationIsValset(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.QgbKeeper

	// EndBlocker should set a new validator set
	qgb.EndBlocker(ctx, *pk)

	require.Equal(t, uint64(1), pk.GetLatestAttestationNonce(ctx))
	attestation, found, err := pk.GetAttestationByNonce(ctx, 1)
	require.Nil(t, err)
	require.True(t, found)
	require.NotNil(t, attestation)
	require.Equal(t, uint64(1), attestation.GetNonce())

	// get the valset
	require.Equal(t, types.ValsetRequestType, attestation.Type())
	vs, ok := attestation.(*types.Valset)
	require.True(t, ok)
	require.NotNil(t, vs)
}

func TestValsetCreationWhenValidatorUnbonds(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.QgbKeeper

	// run abci methods after chain init
	staking.EndBlocker(input.Context, input.StakingKeeper)
	qgb.EndBlocker(ctx, *pk)

	// current attestation expectedNonce should be 1 because a valset has been emitted upon chain init.
	currentAttestationNonce := pk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	input.Context = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	msgServer := stakingkeeper.NewMsgServerImpl(input.StakingKeeper)

	undelegateMsg := testutil.NewTestMsgUnDelegateValidator(testutil.ValAddrs[0], testutil.StakingAmount)
	_, err := msgServer.Undelegate(input.Context, undelegateMsg)
	require.NoError(t, err)
	staking.EndBlocker(input.Context, input.StakingKeeper)
	qgb.EndBlocker(input.Context, *pk)
	input.Context = ctx.WithBlockHeight(ctx.BlockHeight() + 10)

	assert.Equal(t, currentAttestationNonce+1, pk.GetLatestAttestationNonce(ctx))
}

func TestValsetCreationWhenEditingEVMAddr(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.QgbKeeper

	// run abci methods after chain init
	staking.EndBlocker(input.Context, input.StakingKeeper)
	qgb.EndBlocker(ctx, *pk)

	// current attestation expectedNonce should be 1 because a valset has been emitted upon chain init.
	currentAttestationNonce := pk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	input.Context = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	msgServer := stakingkeeper.NewMsgServerImpl(input.StakingKeeper)

	newEVMAddr, err := teststaking.RandomEVMAddress()
	require.NoError(t, err)
	editMsg := stakingtypes.NewMsgEditValidator(
		testutil.ValAddrs[1],
		stakingtypes.Description{},
		nil,
		nil,
		newEVMAddr,
	)
	_, err = msgServer.EditValidator(input.Context, editMsg)
	require.NoError(t, err)
	staking.EndBlocker(input.Context, input.StakingKeeper)
	qgb.EndBlocker(input.Context, *pk)
	input.Context = ctx.WithBlockHeight(ctx.BlockHeight() + 10)

	assert.Equal(t, currentAttestationNonce+1, pk.GetLatestAttestationNonce(ctx))
}

func TestSetValset(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.QgbKeeper

	vs, err := pk.GetCurrentValset(ctx)
	require.Nil(t, err)
	err = pk.SetAttestationRequest(ctx, &vs)
	require.Nil(t, err)

	require.Equal(t, uint64(1), pk.GetLatestAttestationNonce(ctx))
}

func TestSetDataCommitment(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.QgbKeeper

	ctx = ctx.WithBlockHeight(int64(qk.GetDataCommitmentWindowParam(ctx)))
	dc, err := qk.GetCurrentDataCommitment(ctx)
	require.NoError(t, err)
	err = qk.SetAttestationRequest(ctx, &dc)
	require.NoError(t, err)

	require.Equal(t, uint64(1), qk.GetLatestAttestationNonce(input.Context))
}

func TestGetDataCommitment(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.QgbKeeper

	dcWindow := qk.GetDataCommitmentWindowParam(ctx)

	// get the first data commitment
	ctx = ctx.WithBlockHeight(int64(dcWindow))
	dc1, err := qk.GetCurrentDataCommitment(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), dc1.BeginBlock)
	require.Equal(t, uint64(dcWindow), dc1.EndBlock)
	require.Equal(t, uint64(1), dc1.Nonce)
	// set the first data commitment
	err = qk.SetAttestationRequest(ctx, &dc1)
	require.Nil(t, err)

	// get the second data commitment
	ctx = ctx.WithBlockHeight(int64(dcWindow) * 2)
	dc2, err := qk.GetCurrentDataCommitment(ctx)
	require.Nil(t, err)
	require.Equal(t, dc1.EndBlock+1, dc2.BeginBlock)
	require.Equal(t, dc1.EndBlock+dcWindow, dc2.EndBlock)
	require.Equal(t, uint64(2), dc2.Nonce)
	// setting the second data commitment
	err = qk.SetAttestationRequest(ctx, &dc2)
	require.Nil(t, err)

	// shrinking the data commitment window
	genesis := types.DefaultGenesis()
	newShrinkedDCWindow := dcWindow - 299 // 101, since the default one is 400
	genesis.Params.DataCommitmentWindow = newShrinkedDCWindow
	qk.SetParams(ctx, *genesis.Params)
	require.Equal(t, newShrinkedDCWindow, qk.GetDataCommitmentWindowParam(ctx))

	// getting the third data commitment window
	wantedHeight := nextMultiple(int64(newShrinkedDCWindow), int64(dcWindow)*2)
	// to simulate the condition in endBlocker: ctx.BlockHeight()%int64(k.GetDataCommitmentWindowParam(ctx))
	ctx = ctx.WithBlockHeight(wantedHeight)
	dc3, err := qk.GetCurrentDataCommitment(ctx)
	require.Nil(t, err)
	require.Equal(t, dc2.EndBlock+1, dc3.BeginBlock)
	require.Equal(t, dc2.EndBlock+newShrinkedDCWindow, dc3.EndBlock)
	require.Equal(t, uint64(3), dc3.Nonce)
	// setting the third data commitment
	err = qk.SetAttestationRequest(ctx, &dc3)
	require.Nil(t, err)

	// expanding the data commitment window
	newExpandedDCWindow := dcWindow + 100 // 500, since the default one is 400
	genesis.Params.DataCommitmentWindow = newExpandedDCWindow
	qk.SetParams(ctx, *genesis.Params)
	require.Equal(t, newExpandedDCWindow, qk.GetDataCommitmentWindowParam(ctx))

	// getting the fourth data commitment window
	wantedHeight = nextMultiple(int64(newShrinkedDCWindow), wantedHeight)
	// to simulate the condition in endBlocker: ctx.BlockHeight()%int64(k.GetDataCommitmentWindowParam(ctx))
	ctx = ctx.WithBlockHeight(wantedHeight)
	dc4, err := qk.GetCurrentDataCommitment(ctx)
	require.Nil(t, err)
	require.Equal(t, dc3.EndBlock+1, dc4.BeginBlock)
	require.Equal(t, dc3.EndBlock+newExpandedDCWindow, dc4.EndBlock)
	require.Equal(t, uint64(4), dc4.Nonce)
}

func TestDataCommitmentCreation(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.QgbKeeper

	// run abci methods after chain init
	staking.EndBlocker(input.Context, input.StakingKeeper)
	qgb.EndBlocker(ctx, *qk)

	// current attestation nonce should be 1 because a valset has been emitted upon chain init.
	currentAttestationNonce := qk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	// increment height to be the same as the data commitment window
	newHeight := int64(qk.GetDataCommitmentWindowParam(ctx))
	input.Context = ctx.WithBlockHeight(newHeight)
	qgb.EndBlocker(input.Context, *qk)

	require.Less(t, newHeight, ctx.BlockHeight())
	assert.Equal(t, uint64(2), qk.GetLatestAttestationNonce(ctx))
}

func TestDataCommitmentRange(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	qk := input.QgbKeeper

	// run abci methods after chain init
	staking.EndBlocker(input.Context, input.StakingKeeper)
	qgb.EndBlocker(ctx, *qk)

	// current attestation nonce should be 1 because a valset has been emitted upon chain init.
	currentAttestationNonce := qk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	// increment height to be the same as the data commitment window
	newHeight := int64(qk.GetDataCommitmentWindowParam(ctx))
	input.Context = ctx.WithBlockHeight(newHeight)
	qgb.EndBlocker(input.Context, *qk)

	require.Less(t, newHeight, ctx.BlockHeight())
	assert.Equal(t, uint64(2), qk.GetLatestAttestationNonce(ctx))

	att1, found, err := qk.GetAttestationByNonce(input.Context, 2)
	require.NoError(t, err)
	require.True(t, found)

	assert.Equal(t, types.DataCommitmentRequestType, att1.Type())
	dc1, ok := att1.(*types.DataCommitment)
	require.True(t, ok)
	assert.Equal(t, newHeight, int64(dc1.EndBlock))
	assert.Equal(t, int64(1), int64(dc1.BeginBlock))

	// increment height to 2*data commitment window
	newHeight = int64(qk.GetDataCommitmentWindowParam(ctx)) * 2
	input.Context = ctx.WithBlockHeight(newHeight)
	qgb.EndBlocker(input.Context, *qk)

	att2, found, err := qk.GetAttestationByNonce(input.Context, 3)
	require.NoError(t, err)
	require.True(t, found)

	assert.Equal(t, types.DataCommitmentRequestType, att2.Type())
	dc2, ok := att2.(*types.DataCommitment)
	require.True(t, ok)
	assert.Equal(t, newHeight, int64(dc2.EndBlock))
	assert.Equal(t, dc1.EndBlock+1, dc2.BeginBlock)
}

// nextMultiple calculates the next multiple of the base starting from num.
// for example, nextMultiple(10, 101) will return 110.
func nextMultiple(base, num int64) int64 {
	remainder := num % base
	if remainder == 0 {
		return num
	}
	return num + base - remainder
}
