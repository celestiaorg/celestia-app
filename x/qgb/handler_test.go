package qgb

import (
	"bytes"
	"github.com/celestiaorg/celestia-app/x/qgb/keeper"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestMsgSetOrchestratorAddresses(t *testing.T) {
	var (
		ethAddress, _                 = types.NewEthAddress("0xb462864E395d88d6bc7C5dd5F3F5eb4cc2599255")
		cosmosAddress  sdk.AccAddress = bytes.Repeat([]byte{0x1}, 20)
		ethAddress2, _                = types.NewEthAddress("0x26126048c706fB45a5a6De8432F428e794d0b952")
		cosmosAddress2 sdk.AccAddress = bytes.Repeat([]byte{0x2}, 20)
		blockTime                     = time.Date(2020, 9, 14, 15, 20, 10, 0, time.UTC)
		blockTime2                    = time.Date(2020, 9, 15, 15, 20, 10, 0, time.UTC)
		blockHeight    int64          = 200
		blockHeight2   int64          = 210
	)
	input, ctx := keeper.SetupTestChain(t, []uint64{1000000000}, false)
	wctx := sdk.WrapSDKContext(ctx)
	k := input.QgbKeeper
	h := NewHandler(*input.QgbKeeper)
	ctx = ctx.WithBlockTime(blockTime)
	valAddress, err := sdk.ValAddressFromBech32(input.StakingKeeper.GetValidators(ctx, 10)[0].OperatorAddress)
	require.NoError(t, err)

	// test setting keys
	msg := types.NewMsgSetOrchestratorAddress(valAddress, cosmosAddress, *ethAddress)
	ctx = ctx.WithBlockTime(blockTime).WithBlockHeight(blockHeight)
	_, err = h(ctx, msg)
	require.NoError(t, err)

	// test all lookup methods

	// individual lookups
	ethLookup, found := k.GetEthAddressByValidator(ctx, valAddress)
	assert.True(t, found)
	assert.Equal(t, ethLookup, ethAddress)

	valLookup, found := k.GetOrchestratorValidator(ctx, cosmosAddress)
	assert.True(t, found)
	assert.Equal(t, valLookup.GetOperator(), valAddress)

	// query endpoints
	queryO := types.QueryGetDelegateKeysByOrchestratorAddress{
		OrchestratorAddress: cosmosAddress.String(),
	}
	_, err = k.GetDelegateKeyByOrchestrator(wctx, &queryO)
	require.NoError(t, err)

	// try to set values again. This should fail see issue #344 for why allowing this
	// would require keeping a history of all validators delegate keys forever
	msg = types.NewMsgSetOrchestratorAddress(valAddress, cosmosAddress2, *ethAddress2)
	ctx = ctx.WithBlockTime(blockTime2).WithBlockHeight(blockHeight2)
	_, err = h(ctx, msg)
	require.Error(t, err)
}

// TestMsgValsetConfirm ensures that the valset confirm message sets a validator set confirm
// in the store
func TestMsgValsetConfirm(t *testing.T) {
	var (
		blockTime          = time.Date(2020, 9, 14, 15, 20, 10, 0, time.UTC)
		blockHeight  int64 = 200
		signature          = "7c331bd8f2f586b04a2e2cafc6542442ef52e8b8be49533fa6b8962e822bc01e295a62733abfd65a412a8de8286f2794134c160c27a2827bdb71044b94b003cc1c"
		ethAddress         = "0xd62FF457C6165FF214C1658c993A8a203E601B03"
		wrongAddress       = "0xb9a2c7853F181C3dd4a0517FCb9470C0f709C08C"
	)
	ethAddressParsed, err := types.NewEthAddress(ethAddress)
	require.NoError(t, err)

	input, ctx := keeper.SetupFiveValChain(t)
	k := input.QgbKeeper
	h := NewHandler(*input.QgbKeeper)

	// set a validator set in the store
	vs, err := k.GetCurrentValset(ctx)
	require.NoError(t, err)
	vs.Height = uint64(1)
	vs.Nonce = uint64(1)
	k.StoreValset(ctx, vs)
	k.SetEthAddressForValidator(input.Context, keeper.ValAddrs[0], *ethAddressParsed)

	// try wrong eth address
	msg := &types.MsgValsetConfirm{
		Nonce:        1,
		Orchestrator: keeper.OrchAddrs[0].String(),
		EthAddress:   wrongAddress,
		Signature:    signature,
	}
	ctx = ctx.WithBlockTime(blockTime).WithBlockHeight(blockHeight)
	_, err = h(ctx, msg)
	require.Error(t, err)

	// try a nonexisting valset
	msg = &types.MsgValsetConfirm{
		Nonce:        10,
		Orchestrator: keeper.OrchAddrs[0].String(),
		EthAddress:   ethAddress,
		Signature:    signature,
	}
	ctx = ctx.WithBlockTime(blockTime).WithBlockHeight(blockHeight)
	_, err = h(ctx, msg)
	require.Error(t, err)

	msg = &types.MsgValsetConfirm{
		Nonce:        1,
		Orchestrator: keeper.OrchAddrs[0].String(),
		EthAddress:   ethAddress,
		Signature:    signature,
	}
	ctx = ctx.WithBlockTime(blockTime).WithBlockHeight(blockHeight)
	_, err = h(ctx, msg)
	require.NoError(t, err)
}

//// TestMsgDataCommitmentConfirm ensures that the data commitment confirm message sets a commitment in the store
//func TestMsgDataCommitmentConfirm(t *testing.T) {
//	var (
//		blockTime                    = time.Date(2020, 9, 14, 15, 20, 10, 0, time.UTC)
//		blockHeight   int64          = 200
//		cosmosAddress sdk.AccAddress = bytes.Repeat([]byte{0x5}, 20)
//		signature                    = "7c331bd8f2f586b04a2e2cafc6542442ef52e8b8be49533fa6b8962e822bc01e295a62733abfd65a412a8de8286f2794134c160c27a2827bdb71044b94b003cc1c"
//		ethAddress                   = "0xb462864E395d88d6bc7C5dd5F3F5eb4cc2599256"
//	)
//	ethAddressParsed, err := types.NewEthAddress(ethAddress)
//	require.NoError(t, err)
//
//	input, ctx := keeper.SetupFiveValChain(t)
//	k := input.QgbKeeper
//
//	h := NewHandler(*input.QgbKeeper)
//
//	ctx = ctx.WithBlockTime(blockTime)
//	valAddress, err := sdk.ValAddressFromBech32(input.StakingKeeper.GetValidators(ctx, 10)[3].OperatorAddress)
//	require.NoError(t, err)
//
//	//test setting keys
//	res, _ := k.GetOrchestratorValidator(ctx, cosmosAddress)
//	print(res.String())
//	setOrchMsg := types.NewMsgSetOrchestratorAddress(valAddress, cosmosAddress, *ethAddressParsed)
//	ctx = ctx.WithBlockTime(blockTime).WithBlockHeight(blockHeight)
//	_, err = h(ctx, setOrchMsg)
//	require.NoError(t, err)
//	k.SetEthAddressForValidator(input.Context, valAddress, *ethAddressParsed)
//
//	setDCCMsg := &types.MsgDataCommitmentConfirm{
//		Signature:        signature,
//		ValidatorAddress: cosmosAddress.String(),
//		EthAddress:       ethAddress,
//		Commitment:       "commitment",
//		BeginBlock:       1,
//		EndBlock:         100,
//	}
//	_, err = h(ctx, setDCCMsg)
//	require.NoError(t, err)
//
//	commitment := k.GetDataCommitmentConfirm(ctx, "commitment", keeper.AccAddrs[0])
//	assert.Equal(t, setDCCMsg, commitment)
//}
